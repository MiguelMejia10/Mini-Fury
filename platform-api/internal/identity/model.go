// Package identity owns authentication, authorization, sessions and their repository port.
package identity

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrCredentials = errors.New("invalid credentials")
var ErrUnauthorized = errors.New("unauthorized")
var ErrForbidden = errors.New("forbidden")
var ErrBusy = errors.New("authentication capacity reached")
var ErrRate = errors.New("too many attempts")
var ErrConflict = errors.New("user exists")

type User struct {
	ID     string `json:"id"`
	Active bool   `json:"active"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
}
type Credential struct {
	User
	PasswordHash string
	Version      int64
}
type NewUser struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Password string `json:"password"`
}

func (in *NewUser) Validate() error {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	addr, err := mail.ParseAddress(in.Email)
	if err != nil || addr.Address != in.Email || len(in.Email) > 254 || in.Name == "" || utf8.RuneCountInString(in.Name) > 120 {
		return errors.New("Nombre o correo inválido.")
	}
	for _, r := range in.Name {
		if unicode.IsControl(r) {
			return errors.New("Nombre inválido.")
		}
	}
	if in.Role != "admin" && in.Role != "developer" {
		return errors.New("Rol inválido.")
	}
	if utf8.RuneCountInString(in.Password) < 12 || len(in.Password) > 128 {
		return errors.New("La contraseña debe tener al menos 12 caracteres y como máximo 128 bytes.")
	}
	return nil
}

type Event struct {
	ID         int64     `json:"id"`
	ActorEmail string    `json:"actorEmail"`
	Action     string    `json:"action"`
	ResourceID string    `json:"resourceId"`
	CreatedAt  time.Time `json:"createdAt"`
}
type Repository interface {
	Credentials(context.Context, string) (Credential, error)
	AllowLogin(context.Context, string, string) (bool, error)
	StartSession(context.Context, User, string, string, time.Time, int64) error
	Session(context.Context, string) (User, error)
	Logout(context.Context, User, string) error
	CreateUser(context.Context, *User, NewUser, string) (User, error)
	Users(context.Context, int, int) ([]User, error)
	Audit(context.Context, int, int) ([]Event, error)
	Cleanup(context.Context) error
	ChangePassword(context.Context, User, string, string) error
	RevokeAll(context.Context, User) error
	SetActive(context.Context, User, string, bool) (User, error)
	SearchAudit(context.Context, AuditFilter, int, int) ([]Event, error)
}
type principalKey struct{}

func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, principalKey{}, u)
}
func CurrentUser(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(principalKey{}).(User)
	return u, ok && u.ID != ""
}
