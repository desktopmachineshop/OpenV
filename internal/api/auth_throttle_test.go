package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeLoginService accepts one password and refuses everything else, so the
// tests can drive both outcomes without a database.
type fakeLoginService struct {
	users.Service
	registered int
}

func (f *fakeLoginService) Login(email, password string) (*users.User, string, error) {
	if password == "right" {
		return &users.User{ID: "u1", Email: email}, "session-token", nil
	}
	return nil, "", users.ErrInvalidCredentials
}

func (f *fakeLoginService) Register(email, password, name string) (*users.User, error) {
	f.registered++
	return &users.User{ID: "u2", Email: email}, nil
}

func loginReq(email, password string) *http.Request {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(body)))
	r.RemoteAddr = "203.0.113.7:4242"
	return r
}

func TestLoginThrottlesAnAccountAfterRepeatedFailures(t *testing.T) {
	h := &Handler{
		userService:        &fakeLoginService{},
		authIPLimiter:      newRateLimiter(100, 1),
		authAccountLimiter: newRateLimiter(3, 1),
	}
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.Login(rec, loginReq("Dave@Example.com", "wrong"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, rec.Code)
		}
	}
	// The fourth wrong password is refused before the password is checked,
	// and so is the right one: the account is closed until the bucket refills.
	for _, pw := range []string{"wrong", "right"} {
		rec := httptest.NewRecorder()
		h.Login(rec, loginReq("dave@example.com", pw))
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("password %q after lockout: %d %v", pw, rec.Code, rec.Header())
		}
	}
	// A different account from the same address is untouched: only the
	// address bucket, which has plenty left, applies to it.
	rec := httptest.NewRecorder()
	h.Login(rec, loginReq("other@example.com", "right"))
	if rec.Code != http.StatusOK {
		t.Fatalf("other account: %d", rec.Code)
	}
}

func TestLoginSuccessDoesNotChargeTheAccount(t *testing.T) {
	h := &Handler{
		userService:        &fakeLoginService{},
		authIPLimiter:      newRateLimiter(100, 1),
		authAccountLimiter: newRateLimiter(2, 1),
	}
	for i := 0; i < 10; i++ {
		rec := httptest.NewRecorder()
		h.Login(rec, loginReq("dave@example.com", "right"))
		if rec.Code != http.StatusOK {
			t.Fatalf("sign-in %d: %d", i+1, rec.Code)
		}
	}
}

func TestLoginThrottlesAnAddressAcrossAccounts(t *testing.T) {
	h := &Handler{
		userService:        &fakeLoginService{},
		authIPLimiter:      newRateLimiter(2, 1),
		authAccountLimiter: newRateLimiter(100, 1),
	}
	codes := []int{}
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.Login(rec, loginReq("user"+string(rune('a'+i))+"@example.com", "wrong"))
		codes = append(codes, rec.Code)
	}
	if codes[0] != http.StatusUnauthorized || codes[1] != http.StatusUnauthorized || codes[2] != http.StatusTooManyRequests {
		t.Fatalf("codes %v", codes)
	}
}

func TestRegisterThrottlesAnAddress(t *testing.T) {
	svc := &fakeLoginService{}
	h := &Handler{userService: svc, registerIPLimiter: newRateLimiter(1, 1)}
	body := `{"email":"new@example.com","password":"password1","name":"New"}`
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	r.RemoteAddr = "203.0.113.9:1"
	h.Register(rec, r)
	if svc.registered != 1 {
		t.Fatalf("first registration not attempted: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	r.RemoteAddr = "203.0.113.9:2"
	h.Register(rec, r)
	if rec.Code != http.StatusTooManyRequests || svc.registered != 1 {
		t.Fatalf("second registration: code %d attempts %d", rec.Code, svc.registered)
	}
}

func TestNilThrottlesAllowEverything(t *testing.T) {
	h := &Handler{userService: &fakeLoginService{}}
	for i := 0; i < 50; i++ {
		rec := httptest.NewRecorder()
		h.Login(rec, loginReq("dave@example.com", "wrong"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, rec.Code)
		}
	}
}
