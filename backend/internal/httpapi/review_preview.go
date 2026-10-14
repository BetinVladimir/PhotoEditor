package httpapi

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type ReviewPreviewProvider interface {
	Preview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string) (review.CandidatePreview, error)
}

type PendingReviewPreviewProvider interface {
	IssuePendingPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string, []byte, string) (review.CandidatePreview, error)
	ReadPendingPreview(context.Context, []byte, string, time.Time) ([]byte, string, error)
}

func (s *Server) servePendingReviewMedia(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	provider, ok := s.ReviewPreviews.(PendingReviewPreviewProvider)
	if !ok || len(s.SelectedPreviewKey) < 32 {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	query := r.URL.Query()
	if len(query) != 2 || len(query["token"]) != 1 || len(query["versionId"]) != 1 ||
		query.Get("token") == "" || query.Get("versionId") == "" || r.Header.Get("Range") != "" {
		writeError(w, r, domain.NewError("NOT_FOUND", 404))
		return
	}
	now := time.Now()
	grant, err := review.OpenPendingPreviewGrant(s.SelectedPreviewKey, query.Get("token"), now)
	if err != nil || grant.ObjectVersion != query.Get("versionId") {
		writeError(w, r, domain.NewError("NOT_FOUND", 404))
		return
	}
	data, mediaType, err := provider.ReadPendingPreview(r.Context(), s.SelectedPreviewKey, query.Get("token"), now)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !validReviewMediaBody(grant.Role, data, mediaType) {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	if err := r.Context().Err(); err != nil {
		writeError(w, r, err)
		return
	}
	if reviewMediaGrantExpired(grant.ExpiresUnix, time.Now()) {
		writeError(w, r, domain.NewError("NOT_FOUND", 404))
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

type SelectedRenderPreviewProvider interface {
	SelectedRenderPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string) (review.CandidatePreview, error)
}

type RevocableRenderPreviewProvider interface {
	IssueSelectedRenderPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, []byte, string) (review.CandidatePreview, error)
	ReadSelectedRenderPreview(context.Context, []byte, string, time.Time) ([]byte, string, error)
}

func (s *Server) serveSelectedRenderMedia(w http.ResponseWriter, r *http.Request) {
	s.serveSelectedMedia(w, r, "render")
}

func (s *Server) serveSelectedRestorationMedia(w http.ResponseWriter, r *http.Request) {
	s.serveSelectedMedia(w, r, "restoration")
}

func (s *Server) serveSelectedMedia(w http.ResponseWriter, r *http.Request, kind string) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	provider, ok := s.ReviewPreviews.(RevocableRenderPreviewProvider)
	values := r.URL.Query()
	if !ok || len(s.SelectedPreviewKey) < 32 {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	if len(values) != 2 || len(values["token"]) != 1 || len(values["versionId"]) != 1 ||
		values.Get("token") == "" || values.Get("versionId") == "" || r.Header.Get("Range") != "" {
		writeError(w, r, domain.NewError("NOT_FOUND", 404))
		return
	}
	now := time.Now()
	grant, err := review.OpenSelectedPreviewGrant(s.SelectedPreviewKey, values.Get("token"), now)
	if err != nil || grant.Kind != kind || grant.ObjectVersion != values.Get("versionId") {
		writeError(w, r, domain.NewError("NOT_FOUND", 404))
		return
	}
	data, mediaType, err := provider.ReadSelectedRenderPreview(r.Context(), s.SelectedPreviewKey, values.Get("token"), now)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !validReviewMediaBody(grant.Role, data, mediaType) {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	if err := r.Context().Err(); err != nil {
		writeError(w, r, err)
		return
	}
	if reviewMediaGrantExpired(grant.ExpiresUnix, time.Now()) {
		writeError(w, r, domain.NewError("NOT_FOUND", 404))
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

type SelectedRestorationPreviewProvider interface {
	IssueSelectedRestorationPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, []byte, string) (review.CandidatePreview, error)
}

func validReviewMediaBody(role string, data []byte, mediaType string) bool {
	if len(data) < 2 {
		return false
	}
	switch role {
	case "manifest":
		return len(data) <= 1048576 && mediaType == "application/json"
	case "image":
		return len(data) <= 52*1048576 && (mediaType == "image/jpeg" || mediaType == "image/webp")
	default:
		return false
	}
}

func reviewMediaGrantExpired(expiresUnix int64, now time.Time) bool {
	return expiresUnix <= now.Unix()
}

func (s *Server) serveSelectedRestorationPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	who, tid, err := s.tenantRequest(r, false)
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, err := routeID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	variant := chi.URLParam(r, "variant")
	if variant != "preview1600" && variant != "preview2560" {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	provider, ok := s.ReviewPreviews.(SelectedRestorationPreviewProvider)
	if !ok {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	out, err := provider.IssueSelectedRestorationPreview(r.Context(), who, tid, id, variant, s.SelectedPreviewKey, s.Config.APIPublicOrigin)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !validReviewPreviewResponse(out, id, out.Side, variant, time.Now()) {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	if err := r.Context().Err(); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) serveSelectedRenderPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	who, tid, err := s.tenantRequest(r, false)
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, err := routeID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	variant := chi.URLParam(r, "variant")
	if variant != "preview1600" && variant != "preview2560" {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	provider, ok := s.ReviewPreviews.(RevocableRenderPreviewProvider)
	if !ok {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	out, err := provider.IssueSelectedRenderPreview(r.Context(), who, tid, id, variant, s.SelectedPreviewKey, s.Config.APIPublicOrigin)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !validReviewPreviewResponse(out, id, out.Side, variant, time.Now()) {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	if err := r.Context().Err(); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) serveReviewPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	who, tid, err := s.tenantRequest(r, false)
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	side, variant := chi.URLParam(r, "side"), chi.URLParam(r, "variant")
	if err != nil || id == uuid.Nil || (side != "before" && side != "after") || (variant != "preview1600" && variant != "preview2560") {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	provider, ok := s.ReviewPreviews.(PendingReviewPreviewProvider)
	if !ok {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	out, err := provider.IssuePendingPreview(r.Context(), who, tid, id, side, variant, s.SelectedPreviewKey, s.Config.APIPublicOrigin)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !validReviewPreviewResponse(out, id, side, variant, time.Now()) {
		writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	if err := r.Context().Err(); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// Validate the public wire boundary independently of the installed provider.
// Physical manifest and authorization checks remain the provider's obligation.
func validReviewPreviewResponse(out review.CandidatePreview, id uuid.UUID, side, variant string, now time.Time) bool {
	hash := func(value string) bool {
		if len(value) != 64 || value != strings.ToLower(value) {
			return false
		}
		_, err := hex.DecodeString(value)
		return err == nil
	}
	pin := func(value review.ArtifactRef) bool { return value.ID != uuid.Nil && hash(value.SHA256) }
	edge := 0
	switch variant {
	case "preview1600":
		edge = 1600
	case "preview2560":
		edge = 2560
	default:
		return false
	}
	if id == uuid.Nil || (side != "before" && side != "after") || out.Review != id || out.Side != side || !pin(out.Source) || !pin(out.Manifest.Artifact) || !pin(out.Image.Artifact) || !hash(out.ProfileHash) || out.Width < 1 || out.Height < 1 || out.Width > edge || out.Height > edge {
		return false
	}
	if out.Source.ID == out.Manifest.Artifact.ID || out.Source.ID == out.Image.Artifact.ID || out.Manifest.Artifact.ID == out.Image.Artifact.ID {
		return false
	}
	link := func(value review.CandidatePreviewLink, maxBytes int64) bool {
		_, valid := objectstore.SignedReadVersion(value.URL)
		return valid && value.Bytes >= 2 && value.Bytes <= maxBytes && value.ExpiresAt.After(now) && !value.ExpiresAt.After(now.Add(6*time.Minute))
	}
	return out.Manifest.MediaType == "application/json" && (out.Image.MediaType == "image/jpeg" || out.Image.MediaType == "image/webp") && link(out.Manifest, 1048576) && link(out.Image, 52*1048576)
}
