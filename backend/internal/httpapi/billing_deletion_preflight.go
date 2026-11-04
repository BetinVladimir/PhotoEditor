package httpapi

import (
	"net/http"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/privacy"
)

func serveDeletionPreflight(w http.ResponseWriter, r *http.Request, svc privacy.DeletionPreflightService, s *Server) {
	if r.Context().Err() != nil {
		return
	}
	who, err := s.mutation(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 400))
		return
	}
	if !idempotency.ValidKey(key) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	var request struct{}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := svc.PrepareDeletion(r.Context(), who, key)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, 200, result)
}
