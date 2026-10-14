package httpapi

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/contactsheet"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/portal"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/upload"
	"photoeditor/backend/internal/v2learning"
	"photoeditor/backend/internal/v2render"
	"photoeditor/backend/internal/v2sessions"
	"time"
)

type Server struct {
	OpenEyesPrepare                  OpenEyesPrepare
	OpenEyesSubmit                   OpenEyesSubmit
	OpenEyesSourceCandidates         GlareSourceCandidates
	OpenEyesDonorCandidates          GlareDonorCandidates
	OpenEyesMaskPairs                GlareMaskPairs
	LocalPortraitPrepare             LocalPortraitPrepare
	LocalPortraitSubmit              LocalPortraitSubmit
	ReflectionPrepare                ReflectionPrepare
	ReflectionSubmit                 ReflectionSubmit
	GlareDonorSubmit                 GlareDonorSubmit
	GlareDonorPrepare                GlareDonorPrepare
	GlareDonorCandidates             GlareDonorCandidates
	GlareSourceCandidates            GlareSourceCandidates
	GlareMaskPairs                   GlareMaskPairs
	InterchangeArchiveStatus         InterchangeArchiveStatusReader
	InterchangeArchiveActions        InterchangeArchiveActions
	InterchangeArchiveBegin          InterchangeArchiveBeginner
	InterchangeSnapshots             InterchangeSnapshotPreparer
	ReviewPreviews                   ReviewPreviewProvider
	SelectedPreviewKey               []byte
	ExportSigner                     ExportSigner
	DeliveryCreator                  DeliveryCreator
	InterchangeCreator               InterchangeCreator
	InterchangeReviewCloser          InterchangeReviewCloser
	InterchangeReviewDecider         InterchangeReviewDecider
	InterchangeMaskDecider           InterchangeMaskDecider
	InterchangeImportReviewDecider   InterchangeImportReviewDecider
	InterchangeImportReviewProposals InterchangeImportReviewProposalLister
	InterchangeMaskCandidates        InterchangeMaskCandidateReader
	PerspectiveLinesSubmit           PerspectiveLinesSubmit
	PerspectiveDetectedLines         PerspectiveDetectedLines
	PerspectiveProposals             *v2render.PerspectiveProposalService
	NegativeSubmit                   NegativeSubmit
	PerspectiveSubmit                PerspectiveSubmit
	MultiProposals                   MultiProposalReader
	PanoramaSubmit                   MultiSubmit
	FocusStackSubmit                 MultiSubmit
	RelightAnalysis                  RelightAnalysisSubmit
	RelightSubmit                    RelightSubmit
	SemanticPropose                  SemanticPropose
	SemanticReviewApply              SemanticReviewApply
	SemanticReviewClose              SemanticReviewClose
	RenderFreezer                    RenderFreezer
	RenderQueue                      RenderQueue
	RenderCalibrations               v2render.RenderCalibrationCatalog
	Auth                             *auth.Service
	Config                           config.Config
	DB                               *pgxpool.Pool
	Uploads                          *upload.Service
	Portal                           *portal.Service
	Reviews                          *review.Service
	Media                            *media.Service
	ContactSheets                    *contactsheet.Service
	AnalysisAdmission                v2sessions.AnalysisAdmission
	TrainingAdmission                v2learning.TrainingAdmission
	RegionQueue                      RegionQueue
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Use(s.cors)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Request-ID", middleware.GetReqID(r.Context()))
			bodyLimit := s.Config.MaxJSONBytes
			if isRenderSubmissionPath(r) {
				bodyLimit = v2render.MaxRenderSubmissionBytes
			}
			r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/health/live", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"status": "ok"}) })
	r.Get("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB.Ping(r.Context()); err != nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		writeJSON(w, 200, map[string]any{"status": "ready"})
	})
	r.Get("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"api_version": 1, "locales": []string{"ru", "bg", "en"}, "themes": []string{"light", "dark", "system"}, "registration_enabled": s.Config.RegistrationEnabled, "upload_policy": map[string]any{"max_file_bytes": "536870912", "max_pixels": "200000000", "part_bytes": "16777216"}})
	})
	if s.Auth != nil {
		s.authRoutes(r)
		s.catalogRoutes(r)
		s.deliveryRoutes(r)
		s.provenanceRoutes(r)
		s.interchangeRoutes(r)
		s.semanticReviewRoutes(r)
		s.relightRoutes(r)
		s.glareDonorRoutes(r)
		s.openEyesRoutes(r)
		s.reflectionRoutes(r)
		s.localPortraitRoutes(r)
		s.perspectiveRoutes(r)
		s.negativeRoutes(r)
		s.multiRoutes(r)
		s.multiProposalRoutes(r)
		s.multiReplacementReviewRoutes(r)
		s.perspectiveDetectedLinesRoutes(r)
		s.perspectiveLinesSubmissionRoutes(r)
		s.regionRoutes(r)
		s.renderRoutes(r)
		s.renderFreezeRoutes(r)
	}
	if s.Portal != nil {
		s.portalRoutes(r)
	}
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { writeError(w, r, domain.NewError("NOT_FOUND", 404)) })
	return r
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	e := domain.AsError(err)
	writeJSON(w, e.HTTPStatus, map[string]any{"code": e.Code, "message_key": e.MessageKey, "args": e.Args, "request_id": middleware.GetReqID(r.Context())})
}
func (s *Server) HTTPServer() *http.Server {
	return &http.Server{Addr: s.Config.Listen, Handler: s.Routes(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second}
}
