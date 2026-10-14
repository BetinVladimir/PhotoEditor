package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/interchange"
)

type InterchangeImportReviewProposalLister interface {
	List(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) ([]interchange.ImportReviewProposalItem, error)
}

func (s *Server) importReviewDecisionRoutes(router chi.Router) {
	base := "/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/prepared-reviews"
	router.Get(base+"/saved", func(w http.ResponseWriter, r *http.Request) {
		who, tenantID, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		projectID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflowID, err := routeID(r, "workflow")
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.InterchangeImportReviewDecider == nil {
			writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
			return
		}
		items, err := s.InterchangeImportReviewDecider.ListSaved(r.Context(), who, tenantID, projectID, workflowID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if items == nil {
			items = []interchange.ImportReviewDecisionStatus{}
		}
		writeJSON(w, http.StatusOK, struct {
			WorkflowID uuid.UUID                                `json:"workflow_id"`
			Items      []interchange.ImportReviewDecisionStatus `json:"items"`
		}{workflowID, items})
	})
	router.Get(base, func(w http.ResponseWriter, r *http.Request) {
		who, tenantID, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		projectID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflowID, err := routeID(r, "workflow")
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.InterchangeImportReviewProposals == nil {
			writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
			return
		}
		items, err := s.InterchangeImportReviewProposals.List(r.Context(), who, tenantID, projectID, workflowID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if items == nil {
			items = []interchange.ImportReviewProposalItem{}
		}
		writeJSON(w, http.StatusOK, struct {
			WorkflowID uuid.UUID                              `json:"workflow_id"`
			Items      []interchange.ImportReviewProposalItem `json:"items"`
		}{workflowID, items})
	})
	router.Post(base, func(w http.ResponseWriter, r *http.Request) {
		who, tenantID, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		projectID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflowID, err := routeID(r, "workflow")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveImportReviewDecision(w, r, who, tenantID, projectID, workflowID, s.InterchangeImportReviewDecider)
	})
	router.Get(base+"/{entry}/{target}/{kind}/{proposal}", func(w http.ResponseWriter, r *http.Request) {
		who, tenantID, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		projectID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflowID, err := routeID(r, "workflow")
		if err != nil {
			writeError(w, r, err)
			return
		}
		entryID, err := routeID(r, "entry")
		if err != nil {
			writeError(w, r, err)
			return
		}
		targetID, err := routeID(r, "target")
		if err != nil {
			writeError(w, r, err)
			return
		}
		proposalID, err := routeID(r, "proposal")
		if err != nil {
			writeError(w, r, err)
			return
		}
		kind := chi.URLParam(r, "kind")
		if kind != "defects" && kind != "dust" {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		serveImportReviewStatus(w, r, who, tenantID, projectID, workflowID, entryID, targetID, kind, proposalID, s.InterchangeImportReviewDecider)
	})
}
