package httpapi

import (
	"net/http"
	"photoeditor/backend/internal/domain"
)

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if origin != s.Config.AppOrigin && origin != s.Config.PortalOrigin {
				writeError(w, r, domain.NewError("ORIGIN_DENIED", 403))
				return
			}
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, Retry-After, ETag, X-Preview-Run-ID")
		}
		if r.Method == http.MethodOptions {
			if origin == "" {
				w.WriteHeader(400)
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, X-Photo-Client, X-Photo-Auth-Nonce, Idempotency-Key, Generation-Replay-Only, Relight-Replay-Only, Save-Edits-Resolve-Only, If-Match")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
