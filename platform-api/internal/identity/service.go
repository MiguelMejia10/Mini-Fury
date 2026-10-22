package identity

import (
	"context"
	"strings"
	"time"
)

type Service struct {
	Repo      Repository
	slots     chan struct{}
	dummyHash string
}

func NewService(repo Repository) (*Service, error) {
	dummy, err := Token()
	if err != nil {
		return nil, err
	}
	hash, err := HashPassword(dummy)
	if err != nil {
		return nil, err
	}
	return &Service{Repo: repo, slots: make(chan struct{}, 2), dummyHash: hash}, nil
}
func (s *Service) Login(ctx context.Context, email, password, ip, previous string) (User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) > 254 || len(password) > 128 || email == "" || password == "" {
		return User{}, "", ErrCredentials
	}
	allowed, err := s.Repo.AllowLogin(ctx, Digest("ip:"+ip), Digest("email:"+email))
	if err != nil {
		return User{}, "", err
	}
	if !allowed {
		return User{}, "", ErrRate
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return User{}, "", ErrBusy
	}
	credential, err := s.Repo.Credentials(ctx, email)
	if err != nil && err != ErrCredentials {
		return User{}, "", err
	}
	hash := s.dummyHash
	if err == nil {
		hash = credential.PasswordHash
	}
	valid := VerifyPassword(password, hash)
	if !valid || err != nil || !credential.Active {
		return User{}, "", ErrCredentials
	}
	if err = ctx.Err(); err != nil {
		return User{}, "", err
	}
	token, err := Token()
	if err != nil {
		return User{}, "", err
	}
	err = s.Repo.StartSession(ctx, credential.User, Digest(token), Digest(previous), time.Now().Add(8*time.Hour), credential.Version)
	return credential.User, token, err
}
func (s *Service) CreateUser(ctx context.Context, actor *User, in NewUser) (User, error) {
	if err := in.Validate(); err != nil {
		return User{}, err
	}
	if actor != nil && actor.Role != "admin" {
		return User{}, ErrForbidden
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return User{}, ErrBusy
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, err
	}
	return s.Repo.CreateUser(ctx, actor, in, hash)
}
