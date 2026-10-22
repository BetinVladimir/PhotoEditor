package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/semanticreview"
)

// SemanticReviewPreviewProvider resolves only a canonical, source-pinned
// pre-Apply render and its measured quality/protection evidence. It must check
// live editor ACL and plan/target fences before and after object IO. It cannot
// render by borrowing an approval or return an applied child as a proposal.
type SemanticReviewPreviewProvider interface {
	ReadPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, semanticreview.PreviewRequest) (semanticreview.Preview, error)
}

type SemanticPreviewAdmission interface {
	AdmitPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, semanticreview.PreviewRequest) (semanticreview.PreviewAdmission, error)
}

func (s *Server) semanticPreviewRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/semantic-intents/{id}/targets/{target}/preview", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		intent, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		target, err := routeID(r, "target")
		if err != nil {
			writeError(w, r, err)
			return
		}
		request, err := semanticPreviewQuery(r.URL.RawQuery)
		if err != nil {
			writeError(w, r, err)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		if s.SemanticPreviewAdmission == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		result, err := s.SemanticPreviewAdmission.AdmitPreview(r.Context(), who, tenant, intent, target, request)
		writeSemanticPreviewAdmission(w, r, result, err)
	})

	router.Get("/api/v1/tenants/{t}/semantic-intents/{id}/targets/{target}/preview", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		intent, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		target, err := routeID(r, "target")
		if err != nil {
			writeError(w, r, err)
			return
		}
		request, err := semanticPreviewQuery(r.URL.RawQuery)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if r.Context().Err() != nil {
			return
		}
		if s.SemanticReviewPreview == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		if err := semanticreview.CheckCurrentPreviewAuthority(r.Context(), s.DB, who, tenant, intent, target, request); err != nil {
			writeError(w, r, semanticReviewError(err))
			return
		}
		preview, err := s.SemanticReviewPreview.ReadPreview(r.Context(), who, tenant, intent, target, request)
		if r.Context().Err() != nil {
			return
		}
		if err != nil {
			writeError(w, r, semanticReviewError(err))
			return
		}
		if preview.Validate(tenant, intent, target, request, time.Now()) != nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		if err := semanticreview.CheckCurrentPreviewAuthority(r.Context(), s.DB, who, tenant, intent, target, request); err != nil {
			if r.Context().Err() == nil {
				writeError(w, r, semanticReviewError(err))
			}
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, 200, preview)
	})
}

func semanticPreviewQuery(raw string) (semanticreview.PreviewRequest, error) {
	invalid := func() (semanticreview.PreviewRequest, error) {
		return semanticreview.PreviewRequest{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	params, err := url.ParseQuery(raw)
	if err != nil {
		return invalid()
	}
	for key, values := range params {
		if len(values) != 1 || (key != "expected_revision" && key != "target_revision" && key != "plan_sha" && key != "candidate_sha") {
			return invalid()
		}
	}
	request := semanticreview.PreviewRequest{ExpectedRevision: params.Get("expected_revision"), TargetRevision: params.Get("target_revision"), PlanSHA: params.Get("plan_sha"), CandidateSHA: params.Get("candidate_sha")}
	if request.Validate() != nil {
		return invalid()
	}
	return request, nil
}

// Preserve recovery identity even on an uncertain commit error. Never emit an
// accepted response for a partial draft or an unknown persisted state.
func writeSemanticPreviewAdmission(w http.ResponseWriter, r *http.Request, result semanticreview.PreviewAdmission, err error) {
	if result.RunID != uuid.Nil {
		w.Header().Set("X-Preview-Run-ID", result.RunID.String())
	}
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, semanticReviewError(err))
		return
	}
	if result.RunID == uuid.Nil || result.TaskID == uuid.Nil {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	validStatus := false
	switch result.RunStatus {
	case "queued", "running", "completed", "failed", "cancelled", "cancelling":
		validStatus = true
	}
	if !validStatus || (!result.Recovered && result.RunStatus != "queued") {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	writeJSON(w, 202, map[string]any{"run_id": result.RunID, "task_id": result.TaskID,
		"status": result.RunStatus, "commit_attempted": result.CommitAttempted, "recovered": result.Recovered})
}
