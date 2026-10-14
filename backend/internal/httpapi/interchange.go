package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"photoeditor/backend/internal/interchange"
)

func (s *Server) interchangeRoutes(router chi.Router) {
	s.importReviewDecisionRoutes(router)
	router.Get("/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/person-mask-confirmations", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflow, err := routeID(r, "workflow")
		if err != nil {
			writeError(w, r, err)
			return
		}
		s.serveInterchangeMaskStatus(w, r, who, tid, pid, workflow)
	})
	router.Get("/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/person-mask-candidates/{entry}/previews/{override}/{mask}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflow, err := routeID(r, "workflow")
		if err != nil {
			writeError(w, r, err)
			return
		}
		entry, err := routeID(r, "entry")
		if err != nil {
			writeError(w, r, err)
			return
		}
		overrideID, err := routeID(r, "override")
		if err != nil {
			writeError(w, r, err)
			return
		}
		maskID, err := routeID(r, "mask")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeMaskPreview(w, r, who, tid, pid, workflow, entry, overrideID, maskID, s.InterchangeMaskCandidates)
	})
	router.Get("/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/person-mask-candidates", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflow, err := routeID(r, "workflow")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeMaskCandidateEntries(w, r, who, tid, pid, workflow, s.InterchangeMaskCandidates)
	})
	router.Get("/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/person-mask-candidates/{entry}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflow, err := routeID(r, "workflow")
		if err != nil {
			writeError(w, r, err)
			return
		}
		entry, err := routeID(r, "entry")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeMaskCandidates(w, r, who, tid, pid, workflow, entry, s.InterchangeMaskCandidates)
	})
	router.Post("/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/person-mask-confirmations", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflow, err := routeID(r, "workflow")
		if err != nil {
			writeError(w, r, err)
			return
		}
		decider := s.InterchangeMaskDecider
		if decider == nil {
			decider = interchange.ImportPersonMaskDecisionService{DB: s.DB}
		}
		serveInterchangeMaskDecision(w, r, who, tid, pid, workflow, decider)
	})
	router.Post("/api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/attempt-status", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeArchiveAttemptStatus(w, r, who, tid, pid, s.InterchangeArchiveStatus)
	})
	router.Post("/api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}/abort", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		upload, err := routeID(r, "upload")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeArchiveAbort(w, r, who, tid, pid, upload, s.InterchangeArchiveActions)
	})
	router.Get("/api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		upload, err := routeID(r, "upload")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeArchiveStatus(w, r, who, tid, pid, upload, s.InterchangeArchiveStatus)
	})
	router.Post("/api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}/parts", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		upload, err := routeID(r, "upload")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeArchiveParts(w, r, who, tid, pid, upload, s.InterchangeArchiveActions)
	})
	router.Post("/api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}/complete", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		upload, err := routeID(r, "upload")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeArchiveComplete(w, r, who, tid, pid, upload, s.InterchangeArchiveActions)
	})
	router.Post("/api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeArchiveBegin(w, r, who, tid, pid, s.InterchangeArchiveBegin)
	})
	router.Post("/api/v1/tenants/{t}/projects/{id}/interchange-export-snapshots", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeSnapshot(w, r, who, tid, pid, s.InterchangeSnapshots)
	})
	router.Post("/api/v1/tenants/{t}/interchange-jobs/{id}/decisions", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveDecideInterchangeReview(w, r, who, tid, s.InterchangeReviewDecider)
	})
	router.Post("/api/v1/tenants/{t}/interchange-jobs/{id}/close-review", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveCloseInterchangeReview(w, r, who, tid, s.InterchangeReviewCloser)
	})
	router.Get("/api/v1/tenants/{t}/interchange-jobs/{id}/inspection", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeInspection(w, r, who, tid, interchange.Service{DB: s.DB})
	})
	router.Get("/api/v1/tenants/{t}/interchange-jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		serveInterchangeJob(w, r, who, tid, interchange.Service{DB: s.DB})
	})
	router.Post("/api/v1/tenants/{t}/interchange-jobs", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		creator := s.InterchangeCreator
		if creator == nil {
			creator = InterchangeCreateService{Service: interchange.Service{DB: s.DB}}
		}
		serveCreateInterchange(w, r, who, tid, creator)
	})
}
