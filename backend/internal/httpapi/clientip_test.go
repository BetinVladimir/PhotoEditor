package httpapi

import (
	"net/http/httptest"
	"testing"

	"photoeditor/backend/internal/config"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name, remote, xff string
		trusted           []string
		want              string
	}{
		{"no trust ignores header", "203.0.113.9:1", "1.2.3.4", nil, "203.0.113.9"},
		{"untrusted peer ignores header", "203.0.113.9:1", "1.2.3.4", []string{"10.0.0.0/8"}, "203.0.113.9"},
		{"trusted peer uses xff", "10.0.0.2:1", "198.51.100.7", []string{"10.0.0.0/8"}, "198.51.100.7"},
		{"spoofed prefix skipped", "10.0.0.2:1", "6.6.6.6, 198.51.100.7", []string{"10.0.0.0/8"}, "198.51.100.7"},
		{"trusted chain skipped", "10.0.0.2:1", "198.51.100.7, 10.0.0.5", []string{"10.0.0.0/8"}, "198.51.100.7"},
		{"garbage falls back to peer", "10.0.0.2:1", "nonsense", []string{"10.0.0.0/8"}, "10.0.0.2"},
		{"no header falls back to peer", "10.0.0.2:1", "", []string{"10.0.0.0/8"}, "10.0.0.2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{Config: config.Config{TrustedProxyCIDRs: c.trusted}}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remote
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			if got := s.clientIP(r); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
