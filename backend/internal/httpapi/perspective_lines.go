package httpapi

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2optical"
)

type PerspectiveLinesPublish interface {
	PublishProjectLines(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2optical.PerspectiveLinesIntent) (v2optical.PerspectiveLinesReceipt, error)
}

func (s *Server) perspectiveLinesRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/projects/{id}/perspective-lines", func(w http.ResponseWriter, r *http.Request) {
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
		key := r.Header.Get("Idempotency-Key")
		if !idempotency.ValidKey(key) {
			writeError(w, r, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1048576)
		var raw json.RawMessage
		if err = decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		intent, err := v2optical.DecodePerspectiveLinesIntent(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		service, ok := s.PerspectiveSubmit.(PerspectiveLinesPublish)
		if !ok || service == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		receipt, err := service.PublishProjectLines(r.Context(), who, tenant, project, key, intent)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		if receipt.SchemaVersion != 1 || receipt.Tenant != tenant || receipt.Project != project || receipt.Snapshot != intent.Snapshot || receipt.Base != intent.Base || receipt.Lines.ArtifactID == uuid.Nil || receipt.Lines.ArtifactID == intent.Base.Source.ArtifactID {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		if _, err := domain.ParseDigest(receipt.Lines.SHA256); err != nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		writeJSON(w, 200, receipt)
	})
}
