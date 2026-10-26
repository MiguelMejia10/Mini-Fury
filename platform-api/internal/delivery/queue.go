// Package delivery implements a durable encrypted outbox with bounded retries and leases.
package delivery

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

type Message struct {
	To      string `json:"to,omitempty"`
	Subject string `json:"subject,omitempty"`
	Text    string `json:"text"`
	Event   string `json:"event,omitempty"`
	ID      string `json:"id,omitempty"`
}
type Sender interface {
	Send(context.Context, string, Message) error
}
type Queue struct {
	DB     *sql.DB
	box    cipher.AEAD
	Sender Sender
}

func New(db *sql.DB, key string, sender Sender) (*Queue, error) {
	raw, e := base64.StdEncoding.DecodeString(key)
	if e != nil || len(raw) != 32 {
		return nil, errors.New("DELIVERY_KEY must contain 32 base64-encoded bytes")
	}
	block, e := aes.NewCipher(raw)
	if e != nil {
		return nil, e
	}
	box, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	return &Queue{DB: db, box: box, Sender: sender}, nil
}
func (q *Queue) Enqueue(ctx context.Context, tx *sql.Tx, key, kind string, m Message, expiry time.Time) error {
	raw, e := json.Marshal(m)
	if e != nil {
		return e
	}
	nonce := make([]byte, q.box.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	sealed := q.box.Seal(nonce, nonce, raw, []byte(kind))
	_, e = tx.ExecContext(ctx, `INSERT INTO delivery_jobs(dedupe_key,kind,payload,expires_at) VALUES($1,$2,$3,$4) ON CONFLICT(dedupe_key) DO NOTHING`, key, kind, sealed, expiry)
	return e
}
func (q *Queue) ProcessOne(ctx context.Context) (bool, error) {
	var id, kind string
	var sealed []byte
	var attempts int
	e := q.DB.QueryRowContext(ctx, `WITH candidate AS (SELECT id FROM delivery_jobs WHERE expires_at>now() AND attempts<6 AND available_at<=now() AND (state='pending' OR (state='processing' AND locked_until<now())) ORDER BY available_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE delivery_jobs j SET state='processing',attempts=attempts+1,locked_until=now()+interval '30 seconds' FROM candidate c WHERE j.id=c.id RETURNING j.id::text,j.kind,j.payload,j.attempts`).Scan(&id, &kind, &sealed, &attempts)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var m Message
	sendErr := errors.New("invalid encrypted payload")
	if len(sealed) >= q.box.NonceSize() {
		raw, e := q.box.Open(nil, sealed[:q.box.NonceSize()], sealed[q.box.NonceSize():], []byte(kind))
		if e == nil {
			e = json.Unmarshal(raw, &m)
		}
		if e == nil {
			delivery, cancel := context.WithTimeout(ctx, 10*time.Second)
			sendErr = q.Sender.Send(delivery, kind, m)
			cancel()
		}
	}
	state := "sent"
	reason := ""
	delay := time.Duration(1<<attempts) * time.Second
	if sendErr != nil {
		state = "pending"
		reason = "delivery_failed"
		if attempts >= 6 {
			state = "failed"
		}
	}
	// Never persist provider errors: they may contain addresses, credentials or webhook URLs.
	_, e = q.DB.ExecContext(ctx, `UPDATE delivery_jobs SET state=$2,last_error=$3,available_at=now()+$4*interval '1 second',locked_until=NULL,completed_at=CASE WHEN $2 IN ('sent','failed') THEN now() ELSE NULL END,payload=CASE WHEN $2 IN ('sent','failed') THEN ''::bytea ELSE payload END WHERE id=$1 AND state='processing' AND attempts=$5`, id, state, reason, delay.Seconds(), attempts)
	return true, e
}
func (q *Queue) Run(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	cycles := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for range 10 {
				work, e := q.ProcessOne(ctx)
				if e != nil {
					onError(e)
					break
				}
				if !work {
					break
				}
			}
			cycles++
			if cycles%60 == 0 {
				_, e := q.DB.ExecContext(ctx, `UPDATE delivery_jobs SET state='failed',last_error='expired',payload=''::bytea,completed_at=now() WHERE state IN ('pending','processing') AND (expires_at<=now() OR (attempts>=6 AND locked_until<now())); DELETE FROM delivery_jobs WHERE completed_at<now()-interval '30 days'; DELETE FROM password_resets WHERE expires_at<now(); DELETE FROM alert_transitions WHERE created_at<now()-interval '30 days'`)
				if e != nil {
					onError(e)
				}
			}
		}
	}
}
