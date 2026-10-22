package identity

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRepository struct {
	created    atomic.Int32
	attempts   atomic.Int32
	user       User
	err        error
	credential Credential
}

func (f *fakeRepository) Credentials(context.Context, string) (Credential, error) {
	return f.credential, f.err
}
func (f *fakeRepository) AllowLogin(context.Context, string, string) (bool, error) { return true, nil }
func (f *fakeRepository) StartSession(context.Context, User, string, string, time.Time, int64) error {
	f.created.Add(1)
	return nil
}
func (f *fakeRepository) Session(context.Context, string) (User, error) { return f.user, f.err }
func (f *fakeRepository) Logout(context.Context, User, string) error    { return nil }
func (f *fakeRepository) CreateUser(context.Context, *User, NewUser, string) (User, error) {
	return f.user, f.err
}
func (f *fakeRepository) Users(context.Context, int, int) ([]User, error)  { return []User{}, nil }
func (f *fakeRepository) Audit(context.Context, int, int) ([]Event, error) { return []Event{}, nil }
func (f *fakeRepository) Cleanup(context.Context) error                    { return nil }
func TestPasswordAndToken(t *testing.T) {
	encoded, e := HashPassword("Correct-Horse-Battery-42")
	if e != nil {
		t.Fatal(e)
	}
	if !VerifyPassword("Correct-Horse-Battery-42", encoded) || VerifyPassword("wrong", encoded) || VerifyPassword("password", "bad-hash") {
		t.Fatal("password verification failed")
	}
	token, _ := Token()
	other, _ := Token()
	if token == other || len(token) != 43 || strings.Contains(encoded, "Correct-Horse") {
		t.Fatal("invalid token or hash")
	}
	if CSRF(token) == Digest(token) {
		t.Fatal("CSRF must have domain separation")
	}
}
func TestHashConcurrencyBound(t *testing.T) {
	repo := &fakeRepository{}
	service, err := NewService(repo)
	if err != nil {
		t.Fatal(err)
	}
	service.slots <- struct{}{}
	service.slots <- struct{}{}
	_, _, err = service.Login(context.Background(), "u@example.com", "password", "127.0.0.1", "")
	if !errors.Is(err, ErrBusy) || repo.created.Load() != 0 {
		t.Fatalf("unbounded password work: %v", err)
	}
}
func TestSessionCSRFAndRoleGuards(t *testing.T) {
	repo := &fakeRepository{user: User{ID: "user-id", Role: "developer"}}
	service := &Service{Repo: repo}
	auth := &HTTP{Service: service, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Origins: Origins("https://mini-fury.example"), Secure: true}
	token := strings.Repeat("a", 43)
	for _, tc := range []struct {
		name, method, origin, csrf string
		cookie                     bool
		status                     int
	}{
		{"no session", "GET", "", "", false, 401},
		{"read", "GET", "", "", true, 204},
		{"csrf missing", "POST", "https://mini-fury.example", "", true, 403},
		{"wrong origin", "POST", "https://evil.example", CSRF(token), true, 403},
		{"valid write", "POST", "https://mini-fury.example", CSRF(token), true, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/api/applications", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-CSRF-Token", tc.csrf)
			if tc.cookie {
				r.AddCookie(&http.Cookie{Name: auth.cookieName(), Value: token})
			}
			w := httptest.NewRecorder()
			auth.Protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := CurrentUser(r.Context()); !ok {
					t.Error("missing principal")
				}
				w.WriteHeader(204)
			})).ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
	r := httptest.NewRequest("GET", "/api/admin/users", nil)
	r.AddCookie(&http.Cookie{Name: auth.cookieName(), Value: token})
	w := httptest.NewRecorder()
	auth.admin(auth.users).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("developer accessed admin route")
	}
	repo.err = ErrUnauthorized
	w = httptest.NewRecorder()
	auth.Protected(http.HandlerFunc(auth.me)).ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("expired session accepted")
	}
	w = httptest.NewRecorder()
	auth.setCookie(w, token)
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Name != "__Host-mini_fury_session" {
		t.Fatal("unsafe cookie configuration")
	}
}
func TestPageBounds(t *testing.T) {
	for _, query := range []string{"?limit=0", "?limit=101", "?offset=-1", "?offset=10001", "?offset=foo"} {
		r := httptest.NewRequest("GET", "/"+query, nil)
		if _, _, err := Page(r); err == nil {
			t.Fatalf("unbounded query accepted: %s", query)
		}
	}
}

func (f *fakeRepository) ChangePassword(context.Context, User, string, string) error { return nil }
func (f *fakeRepository) RevokeAll(context.Context, User) error                      { return nil }
func (f *fakeRepository) SetActive(context.Context, User, string, bool) (User, error) {
	return f.user, f.err
}
func (f *fakeRepository) SearchAudit(context.Context, AuditFilter, int, int) ([]Event, error) {
	return []Event{}, nil
}
