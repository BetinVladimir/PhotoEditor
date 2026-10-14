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
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/semanticreview"
)

// SemanticReviewApply executes authenticated, published and source-verified
// review admission; implementations own transaction rollback/commit and replay.
type SemanticReviewApply interface {
	Apply(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, semanticreview.ApplyDecision) (idempotency.Response, error)
}

var _ SemanticReviewApply = (*semanticreview.ApplyService)(nil)

type SemanticReviewClose interface {
	Close(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, semanticreview.ReviewClose) (idempotency.Response, error)
}

var _ SemanticReviewClose = (*semanticreview.CloseService)(nil)

func semanticCloseResponse(response idempotency.Response) (semanticreview.CloseResult, error) {
	result, err := semanticreview.DecodeCloseResult(response.Body)
	if err != nil || response.Status != 200 || response.ResourceID == nil || *response.ResourceID == uuid.Nil {
		return semanticreview.CloseResult{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return result, nil
}

func semanticReviewResponse(response idempotency.Response) (semanticreview.BatchResult, error) {
	result, err := semanticreview.DecodeBatchResult(response.Body)
	if err != nil || response.Status != 200 || response.ResourceID == nil || *response.ResourceID != result.BatchID {
		return semanticreview.BatchResult{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return result, nil
}

func (s *Server) semanticReviewRoutes(router chi.Router) {
	s.semanticProposeRoutes(router)
	router.Get("/api/v1/tenants/{t}/runs/{id}/semantic-intent", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		run, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		result, err := semanticreview.ReadRunIntent(r.Context(), s.DB, who, tid, run)
		if err != nil {
			writeError(w, r, semanticReviewError(err))
			return
		}
		writeJSON(w, 200, result)
	})
	router.Get("/api/v1/tenants/{t}/semantic-intents/{id}/review", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		intent, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		result, err := semanticreview.ReadStatus(r.Context(), s.DB, who, tenant, intent)
		if err != nil {
			err = semanticReviewError(err)
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, result)
	})
	router.Get("/api/v1/tenants/{t}/semantic-intents/{id}/targets", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		intent, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		count, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		reader := semanticreview.TargetReader{DB: s.DB, Cursors: pagination.Codec{Key: auth.MAC(s.Auth.Pepper, "cursor-key")}}
		page, err := reader.List(r.Context(), who, tid, intent, count, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, page)
	})
	router.Post("/api/v1/tenants/{t}/semantic-intents/{id}/apply", func(w http.ResponseWriter, r *http.Request) {
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
		key := r.Header.Get("Idempotency-Key")
		if !idempotency.ValidKey(key) {
			writeError(w, r, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		var raw json.RawMessage
		if err = decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		request, err := semanticreview.DecodeApplyDecision(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.SemanticReviewApply == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		response, err := s.SemanticReviewApply.Apply(r.Context(), who, tenant, intent, key, request)
		if err != nil {
			err = semanticReviewError(err)
			writeError(w, r, err)
			return
		}
		result, err := semanticReviewResponse(response)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, result)
	})
	router.Post("/api/v1/tenants/{t}/semantic-intents/{id}/close-review", func(w http.ResponseWriter, r *http.Request) {
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
		key := r.Header.Get("Idempotency-Key")
		if !idempotency.ValidKey(key) {
			writeError(w, r, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		var raw json.RawMessage
		if err = decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		request, err := semanticreview.DecodeReviewClose(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.SemanticReviewClose == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		response, err := s.SemanticReviewClose.Close(r.Context(), who, tenant, intent, key, request)
		if err != nil {
			err = semanticReviewError(err)
			writeError(w, r, err)
			return
		}
		result, err := semanticCloseResponse(response)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, result)
	})
}
