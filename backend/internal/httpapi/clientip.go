package httpapi

import (
	"net"
	"net/http"
	"strings"
)

// clientIP returns the address used for rate limiting and audit. X-Forwarded-For
// is honoured only when the direct peer is inside Config.TrustedProxyCIDRs; the
// right-most untrusted hop is used so a client cannot spoof an earlier entry.
func (s *Server) clientIP(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	trusted := parseCIDRs(s.Config.TrustedProxyCIDRs)
	if len(trusted) == 0 || !ipInNets(peer, trusted) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if net.ParseIP(hop) == nil {
			return peer
		}
		if !ipInNets(hop, trusted) {
			return hop
		}
	}
	return peer
}

func parseCIDRs(in []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(in))
	for _, c := range in {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func ipInNets(ip string, nets []*net.IPNet) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(p) {
			return true
		}
	}
	return false
}
