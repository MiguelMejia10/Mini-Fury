package platform

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"mini-operador-ti/platform-api/internal/identity"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPostgresPersistenceAndUniqueness(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatalf("migration replay: %v", err)
	}
	name := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	defer db.ExecContext(context.Background(), `DELETE FROM applications WHERE name=$1`, name)
	var actor identity.User
	err = db.QueryRowContext(ctx, `INSERT INTO users(email,name,role,password_hash) VALUES($1,'integration','admin','unused') RETURNING id::text,email,name,role`, name+"@test.local").Scan(&actor.ID, &actor.Email, &actor.Name, &actor.Role)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.ExecContext(context.Background(), `DELETE FROM audit_events WHERE actor_id=$1`, actor.ID)
		db.ExecContext(context.Background(), `DELETE FROM applications WHERE created_by=$1`, actor.ID)
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, actor.ID)
	}()
	ctx = identity.WithUser(ctx, actor)
	store := &PostgresStore{DB: db}
	in := CreateApplication{Name: name, Repository: "https://gitlab.com/test/service", Port: 8080, Environment: "staging", Owner: "integration test", Runtime: "Go"}
	var successes, conflicts atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := store.Create(ctx, in)
			switch e {
			case nil:
				successes.Add(1)
			case ErrConflict:
				conflicts.Add(1)
			default:
				t.Errorf("concurrent insert: %v", e)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 7 {
		t.Fatalf("success=%d conflicts=%d", successes.Load(), conflicts.Load())
	}
	listed, err := store.List(ctx, "staging", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, a := range listed {
		if a.Name == name {
			id = a.ID
		}
	}
	if id == "" {
		t.Fatal("created app missing from list")
	}
	in.Environment = "production"
	if _, err = store.Create(ctx, in); err != nil {
		t.Fatalf("same name in another environment: %v", err)
	}
	// A separate DB connection proves the data is stored outside the API process.
	other, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	got, err := (&PostgresStore{DB: other}).Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != name || got.Environment != "staging" || got.Status != "registered" || got.Port != 8080 || got.CreatedAt.IsZero() {
		t.Fatalf("unexpected persisted application: %+v", got)
	}
}
