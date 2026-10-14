package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
)

func (s *Server) maskPairHandler(w http.ResponseWriter, r *http.Request) {
	who, tenant, err := s.tenantRequest(r, true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	project, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || project == uuid.Nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var raw json.RawMessage
	if err = decodeJSON(r, &raw); err != nil {
		writeError(w, r, err)
		return
	}
	query, err := v2draft.DecodeGlareMaskPairQuery(raw)
	if err != nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	if s.GlareMaskPairs == nil {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	page, err := s.GlareMaskPairs.ListProjectMaskPairs(r.Context(), who,
		tenant, project, query)
	if err != nil {
		writeError(w, r, semanticProposalError(err))
		return
	}
	writeJSON(w, http.StatusOK, page)
}
