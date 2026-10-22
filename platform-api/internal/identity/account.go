package identity

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrAccountConflict = errors.New("account changed or administrator safeguard")
var ErrInvalidPassword = errors.New("La contraseña nueva debe tener al menos 12 caracteres y como máximo 128 bytes, y ser distinta de la actual.")

type PasswordChange struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}
type AuditFilter struct {
	Actor, Action, Resource string
	From, Until             *time.Time
}

func (f *AuditFilter) Validate() error {
	f.Actor = strings.ToLower(strings.TrimSpace(f.Actor))
	f.Action = strings.TrimSpace(f.Action)
	f.Resource = strings.TrimSpace(f.Resource)
	if len(f.Actor) > 254 || len(f.Action) > 80 || len(f.Resource) > 128 || (f.From != nil && f.Until != nil && !f.From.Before(*f.Until)) {
		return errors.New("Filtros de auditoría inválidos.")
	}
	return nil
}
func (s *Service) ChangePassword(ctx context.Context, u User, in PasswordChange) error {
	if utf8.RuneCountInString(in.NewPassword) < 12 || len(in.NewPassword) > 128 || in.NewPassword == in.CurrentPassword {
		return ErrInvalidPassword
	}
	if in.CurrentPassword == "" || len(in.CurrentPassword) > 128 {
		return ErrCredentials
	}
	allowed, err := s.Repo.AllowLogin(ctx, Digest("password-ip:"+u.ID), Digest("password-account:"+u.ID))
	if err != nil {
		return err
	}
	if !allowed {
		return ErrRate
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return ErrBusy
	}
	credential, err := s.Repo.Credentials(ctx, u.Email)
	if err != nil {
		return err
	}
	if credential.ID != u.ID || !credential.Active || !VerifyPassword(in.CurrentPassword, credential.PasswordHash) {
		return ErrCredentials
	}
	hash, err := HashPassword(in.NewPassword)
	if err != nil {
		return err
	}
	return s.Repo.ChangePassword(ctx, u, credential.PasswordHash, hash)
}
