package httpapi

import (
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
)

func recentAuthHeadersUnique(r *http.Request) error {
	if r.URL.RawQuery != "" {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	for _, name := range []string{"Authorization", "Content-Type", "Idempotency-Key", "X-Photo-Auth-Nonce", "X-Photo-Client", "X-CSRF-Token", "Origin", "Sec-Fetch-Site"} {
		if len(r.Header.Values(name)) > 1 {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	return nil
}
func recentAuthRecoveryScope(r *http.Request) (auth.OTPReceiptRequest, error) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return auth.OTPReceiptRequest{}, domain.NewError("VALIDATION_FAILED", 400)
	}
	if !idempotency.ValidKey(key) {
		return auth.OTPReceiptRequest{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	values := r.Header.Values("X-Photo-Auth-Nonce")
	if len(values) != 1 {
		return auth.OTPReceiptRequest{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	nonce, err := base64.RawURLEncoding.DecodeString(values[0])
	if err != nil || len(nonce) != 32 || len(values[0]) != 43 || base64.RawURLEncoding.EncodeToString(nonce) != values[0] {
		return auth.OTPReceiptRequest{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return auth.OTPReceiptRequest{Key: key, Bootstrap: values[0]}, nil
}
func (s *Server) serveRecentAuthChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Context().Err() != nil {
		return
	}
	if err := recentAuthHeadersUnique(r); err != nil {
		writeError(w, r, err)
		return
	}
	who, err := s.mutation(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	receipt, err := recentAuthRecoveryScope(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body struct {
		Purpose string `json:"purpose"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.Auth.RequestRecentAuth(r.Context(), who, body.Purpose, s.clientIP(r), receipt)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		problem := domain.AsError(err)
		if problem.Code == "RATE_LIMITED" {
			retry := 3600
			if value, ok := problem.Args["retry_after_seconds"].(int); ok && value >= 1 && value <= 3600 {
				retry = value
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
		}
		writeError(w, r, err)
		return
	}
	writeJSON(w, 202, result)
}
func (s *Server) serveRecentAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Context().Err() != nil {
		return
	}
	if err := recentAuthHeadersUnique(r); err != nil {
		writeError(w, r, err)
		return
	}
	who, err := s.mutation(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	receipt, err := recentAuthRecoveryScope(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body struct {
		Purpose     string `json:"purpose"`
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	challenge, err := uuid.Parse(body.ChallengeID)
	if err != nil || challenge == uuid.Nil || challenge.String() != body.ChallengeID {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	result, err := s.Auth.VerifyRecentAuth(r.Context(), who, body.Purpose, challenge, body.Code, receipt)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 201, result)
}
