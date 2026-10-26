// Package transport only trusts client addresses supplied by explicitly trusted proxies.
package transport

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

func Proxy(raw string, next http.Handler) (http.Handler, error) {
	var networks []*net.IPNet
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		_, network, e := net.ParseCIDR(strings.TrimSpace(part))
		if e != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS")
		}
		networks = append(networks, network)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		peer := net.ParseIP(host)
		trusted := false
		for _, n := range networks {
			if n.Contains(peer) {
				trusted = true
				break
			}
		}
		if trusted {
			ip := net.ParseIP(r.Header.Get("X-Real-IP"))
			if ip != nil {
				clone := r.Clone(r.Context())
				clone.RemoteAddr = net.JoinHostPort(ip.String(), "0")
				r = clone
			}
		}
		next.ServeHTTP(w, r)
	}), nil
}
