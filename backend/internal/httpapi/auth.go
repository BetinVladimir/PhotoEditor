package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func (s *Server) cookieName(kind string) string {
	if s.Config.Environment == "production" {
		return "__Host-photo_" + kind
	}
	return "photo_dev_" + kind
}
func (s *Server) cookie(w http.ResponseWriter, kind, value string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(kind), Value: value, Path: "/", Secure: s.Config.Environment == "production", HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge})
}
func (s *Server) native(r *http.Request) bool {
	return r.Header.Get("X-Photo-Client") == "native" && r.Header.Get("Origin") == "" && r.Header.Get("Sec-Fetch-Site") == ""
}
func (s *Server) checkOrigin(r *http.Request) error {
	if s.native(r) {
		return nil
	}
	origin := r.Header.Get("Origin")
	if origin != s.Config.AppOrigin && origin != s.Config.PortalOrigin {
		return domain.NewError("ORIGIN_DENIED", 403)
	}
	return nil
}
func (s *Server) bootstrapValid(r *http.Request) bool {
	cookie, err := r.Cookie(s.cookieName("csrf"))
	if err != nil {
		return false
	}
	header := r.Header.Get("X-CSRF-Token")
	if !auth.Equal([]byte(header), []byte(cookie.Value)) {
		return false
	}
	parts := strings.Split(header, ".")
	if len(parts) != 3 {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() >= exp {
		return false
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[2])
	return err == nil && auth.Equal(mac, auth.MAC(s.Auth.Pepper, "csrf-bootstrap", parts[0], parts[1]))
}
func (s *Server) bootstrapGuard(r *http.Request) error {
	if err := s.checkOrigin(r); err != nil {
		return err
	}
	if !s.native(r) && !s.bootstrapValid(r) {
		return domain.NewError("CSRF_INVALID", 403)
	}
	return nil
}
func (s *Server) authRoutes(r chi.Router) {
	r.Get("/api/v1/auth/csrf", func(w http.ResponseWriter, r *http.Request) {
		// Fetch Metadata protects same-origin GET clients without an Origin header.
		if origin := r.Header.Get("Origin"); origin != "" && origin != s.Config.AppOrigin && origin != s.Config.PortalOrigin {
			writeError(w, r, domain.NewError("ORIGIN_DENIED", 403))
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Sec-Fetch-Site") == "same-site" {
			writeError(w, r, domain.NewError("ORIGIN_DENIED", 403))
			return
		}
		if c, e := r.Cookie(s.cookieName("refresh")); e == nil {
			token, expires, err := s.Auth.RenewCSRF(r.Context(), c.Value)
			if err == nil {
				writeJSON(w, 200, map[string]any{"csrf_token": token, "expires_at": expires})
				return
			}
		}
		nonce, err := auth.NewSecret(rand.Reader)
		if err != nil {
			writeError(w, r, err)
			return
		}
		expires := time.Now().Add(10 * time.Minute)
		exp := strconv.FormatInt(expires.Unix(), 10)
		token := nonce + "." + exp + "." + base64.RawURLEncoding.EncodeToString(auth.MAC(s.Auth.Pepper, "csrf-bootstrap", nonce, exp))
		s.cookie(w, "csrf", token, expires, 600)
		writeJSON(w, 200, map[string]any{"csrf_token": token, "expires_at": expires})
	})
	r.Post("/api/v1/auth/otp/request", func(w http.ResponseWriter, r *http.Request) {
		if err := s.bootstrapGuard(r); err != nil {
			writeError(w, r, err)
			return
		}
		var req struct {
			Email  string `json:"email"`
			Locale string `json:"locale"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		value, err := s.Auth.RequestOTP(r.Context(), req.Email, req.Locale, ip)
		if err != nil {
			if domain.AsError(err).HTTPStatus == 429 {
				w.Header().Set("Retry-After", "3600")
			}
			writeError(w, r, err)
			return
		}
		writeJSON(w, 202, value)
	})
	r.Post("/api/v1/auth/otp/verify", func(w http.ResponseWriter, r *http.Request) {
		if err := s.bootstrapGuard(r); err != nil {
			writeError(w, r, err)
			return
		}
		var req struct {
			ChallengeID uuid.UUID `json:"challenge_id"`
			Code        string    `json:"code"`
			Client      string    `json:"client_type"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		if (req.Client == "native") != s.native(r) {
			writeError(w, r, domain.NewError("CLIENT_TYPE_INVALID", 422))
			return
		}
		token, err := s.Auth.VerifyOTP(r.Context(), req.ChallengeID, req.Code, req.Client)
		if err != nil {
			writeError(w, r, err)
			return
		}
		s.sendTokens(w, token, req.Client)
	})
	r.Post("/api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		if err := s.checkOrigin(r); err != nil {
			writeError(w, r, err)
			return
		}
		var req struct {
			Refresh string `json:"refresh_token"`
			Client  string `json:"client_type"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		if (req.Client == "native") != s.native(r) {
			writeError(w, r, domain.NewError("CLIENT_TYPE_INVALID", 422))
			return
		}
		if req.Client == "web" {
			if req.Refresh != "" {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			c, err := r.Cookie(s.cookieName("refresh"))
			if err != nil {
				writeError(w, r, domain.NewError("INVALID_CREDENTIALS", 401))
				return
			}
			req.Refresh = c.Value
		}
		token, err := s.Auth.RotateRefresh(r.Context(), req.Refresh, r.Header.Get("X-CSRF-Token"), req.Client)
		if err != nil {
			writeError(w, r, err)
			return
		}
		s.sendTokens(w, token, req.Client)
	})
	r.Post("/api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		identity, err := s.authenticate(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if err = s.checkOrigin(r); err != nil {
			writeError(w, r, err)
			return
		}
		if !s.native(r) {
			if err = s.Auth.ValidateSessionCSRF(r.Context(), identity.SessionID, r.Header.Get("X-CSRF-Token")); err != nil {
				writeError(w, r, err)
				return
			}
		}
		if err = s.Auth.RevokeSession(r.Context(), identity.UserID, identity.SessionID); err != nil {
			writeError(w, r, err)
			return
		}
		s.cookie(w, "refresh", "", time.Unix(0, 0), -1)
		w.WriteHeader(204)
	})
}
func (s *Server) authenticate(r *http.Request) (auth.Identity, error) {
	raw := r.Header.Get("Authorization")
	if !strings.HasPrefix(raw, "Bearer ") {
		return auth.Identity{}, domain.NewError("INVALID_CREDENTIALS", 401)
	}
	return s.Auth.AuthenticateAccess(r.Context(), strings.TrimPrefix(raw, "Bearer "))
}
func (s *Server) sendTokens(w http.ResponseWriter, token auth.Tokens, client string) {
	if client == "web" {
		s.cookie(w, "refresh", token.Refresh, token.RefreshExpires, int(time.Until(token.RefreshExpires).Seconds()))
		token.Refresh = ""
	}
	writeJSON(w, 200, token)
}
