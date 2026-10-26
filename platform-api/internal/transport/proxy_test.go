package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForwardedAddressOnlyFromTrustedProxy(t *testing.T) {
	var received string
	h, e := Proxy("172.30.50.10/32", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = r.RemoteAddr }))
	if e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct{ peer, want string }{{"192.0.2.1:9000", "192.0.2.1:9000"}, {"172.30.50.10:9000", "203.0.113.9:0"}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = test.peer
		r.Header.Set("X-Real-IP", "203.0.113.9")
		h.ServeHTTP(httptest.NewRecorder(), r)
		if received != test.want {
			t.Fatal(received)
		}
	}
}
