// Package teams owns collaboration and membership authorization.
package teams

import (
	"context"
	"errors"
	"mini-operador-ti/platform-api/internal/identity"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrForbidden = errors.New("team operation forbidden")
var ErrNotFound = errors.New("team resource not found")
var ErrConflict = errors.New("membership conflict")

type Team struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
}
type Member struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
}
type Invitation struct {
	ID        string    `json:"id"`
	TeamID    string    `json:"teamId"`
	TeamName  string    `json:"teamName"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type NewTeam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Invite struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func ValidRole(s string) bool { return s == "admin" || s == "editor" || s == "reader" }
func Clean(s string) bool     { return strings.IndexFunc(s, unicode.IsControl) < 0 }
func (t *NewTeam) Validate() error {
	t.Name = strings.TrimSpace(t.Name)
	t.Description = strings.TrimSpace(t.Description)
	if t.Name == "" || utf8.RuneCountInString(t.Name) > 80 || utf8.RuneCountInString(t.Description) > 500 || !Clean(t.Name) || !Clean(t.Description) {
		return errors.New("Nombre o descripción de equipo inválido.")
	}
	return nil
}
func (i *Invite) Validate() error {
	i.Email = strings.ToLower(strings.TrimSpace(i.Email))
	addr, e := mail.ParseAddress(i.Email)
	if e != nil || addr.Address != i.Email || len(i.Email) > 254 || !ValidRole(i.Role) {
		return errors.New("Correo o rol inválido.")
	}
	return nil
}

type Repository interface {
	List(context.Context, int, int) ([]Team, error)
	Create(context.Context, NewTeam) (Team, error)
	Members(context.Context, string, int, int) ([]Member, error)
	Invite(context.Context, string, Invite) (Invitation, error)
	Invitations(context.Context, int, int) ([]Invitation, error)
	Accept(context.Context, string) error
	RevokeInvitation(context.Context, string, string) error
	TeamInvitations(context.Context, string, int, int) ([]Invitation, error)
	SetMember(context.Context, string, string, string) error
}

func Actor(ctx context.Context) (identity.User, error) {
	u, ok := identity.CurrentUser(ctx)
	if !ok {
		return u, identity.ErrUnauthorized
	}
	return u, nil
}
