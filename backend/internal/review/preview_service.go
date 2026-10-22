package review

import (
	"bytes"
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/objectstore"
)

type CandidatePreviewCatalog interface {
	Lookup(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string) (CandidatePreviewObjects, error)
}
type CandidatePreviewSource interface {
	ReadCandidatePreviewSource(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string) (ArtifactRef, []byte, error)
}
type CandidatePreviewSigner interface {
	SignRead(context.Context, objectstore.Object, time.Duration) (objectstore.ReadURL, error)
}
type RenderSelectionReader interface {
	ReadRenderResultSelection(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (RenderResultSelection, error)
}
type RestorationSelectionReader interface {
	ReadRestorationResultSelection(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (RestorationResultSelection, error)
}
type CandidatePreviewService struct {
	Catalog              CandidatePreviewCatalog
	Source               CandidatePreviewSource
	Selection            RenderSelectionReader
	RestorationSelection RestorationSelectionReader
	Objects              Objects
	Signer               CandidatePreviewSigner
	SRGBSHA              string
	Clock                func() time.Time
}

// SelectedRestorationPreview signs only the route chosen by a decided V28
// review. The second selection read closes the physical IO/signing race.
func (s CandidatePreviewService) SelectedRestorationPreview(ctx context.Context, who auth.Identity, tid, id uuid.UUID, variant string) (CandidatePreview, error) {
	var zero CandidatePreview
	if s.RestorationSelection == nil {
		return zero, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	first, err := s.RestorationSelection.ReadRestorationResultSelection(ctx, who, tid, id)
	if err != nil {
		return zero, err
	}
	if !validRenderPreviewSelection(first, id) {
		return zero, objectstore.ErrIntegrity
	}
	if first.Image == nil || first.Route == "skip" {
		return zero, domain.NewError("NOT_FOUND", 404)
	}
	side := "after"
	if first.Route == "base" {
		side = "before"
	} else if first.Route != "candidate" {
		return zero, objectstore.ErrIntegrity
	}
	selectedCtx := context.WithValue(ctx, allowDecidedReviewContextKey{}, true)
	preview, err := s.Preview(selectedCtx, who, tid, id, side, variant)
	if err != nil {
		return zero, err
	}
	if preview.Source != *first.Image {
		return zero, objectstore.ErrIntegrity
	}
	second, err := s.RestorationSelection.ReadRestorationResultSelection(ctx, who, tid, id)
	if err != nil {
		return zero, err
	}
	if !validRenderPreviewSelection(second, id) || second.Image == nil ||
		second.DecisionHash != first.DecisionHash || second.Route != first.Route ||
		*second.Image != *first.Image {
		return zero, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return preview, nil
}

func validRenderPreviewSelection(value RenderResultSelection, reviewID uuid.UUID) bool {
	if value.SchemaVersion != 1 || value.ReviewID != reviewID {
		return false
	}
	if _, err := domain.ParseDigest(value.DecisionHash); err != nil {
		return false
	}
	switch value.Route {
	case "skip":
		return value.Image == nil
	case "base", "candidate":
		return value.Image != nil && value.Image.valid()
	default:
		return false
	}
}

// SelectedRenderPreview exposes only the decided route's physical preview.
// A skipped review has no selected image, and the selection is re-read after
// signing so a stale candidate cannot cross the response boundary.
func (s CandidatePreviewService) SelectedRenderPreview(ctx context.Context, who auth.Identity, tid, id uuid.UUID, variant string) (CandidatePreview, error) {
	var zero CandidatePreview
	if s.Selection == nil {
		return zero, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	first, err := s.Selection.ReadRenderResultSelection(ctx, who, tid, id)
	if err != nil {
		return zero, err
	}
	if !validRenderPreviewSelection(first, id) {
		return zero, objectstore.ErrIntegrity
	}
	if first.Image == nil || first.Route == "skip" {
		return zero, domain.NewError("NOT_FOUND", 404)
	}
	side := "after"
	if first.Route == "base" {
		side = "before"
	} else if first.Route != "candidate" {
		return zero, objectstore.ErrIntegrity
	}
	selectedCtx := context.WithValue(ctx, allowDecidedReviewContextKey{}, true)
	preview, err := s.Preview(selectedCtx, who, tid, id, side, variant)
	if err != nil {
		return zero, err
	}
	if preview.Source != *first.Image {
		return zero, objectstore.ErrIntegrity
	}
	second, err := s.Selection.ReadRenderResultSelection(ctx, who, tid, id)
	if err != nil {
		return zero, err
	}
	if !validRenderPreviewSelection(second, id) || second.Image == nil || second.DecisionHash != first.DecisionHash || second.Route != first.Route || *second.Image != *first.Image {
		return zero, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return preview, nil
}

type CandidatePreviewLink struct {
	Artifact  ArtifactRef `json:"artifact"`
	Bytes     int64       `json:"bytes"`
	MediaType string      `json:"media_type"`
	URL       string      `json:"url"`
	ExpiresAt time.Time   `json:"expires_at"`
}
type CandidatePreview struct {
	Review      uuid.UUID            `json:"review_id"`
	Side        string               `json:"side"`
	Source      ArtifactRef          `json:"source"`
	ProfileHash string               `json:"profile_hash"`
	Width       int                  `json:"width"`
	Height      int                  `json:"height"`
	Manifest    CandidatePreviewLink `json:"manifest"`
	Image       CandidatePreviewLink `json:"image"`
}

func (s CandidatePreviewService) Preview(ctx context.Context, who auth.Identity, tid, id uuid.UUID, side, variant string) (CandidatePreview, error) {
	var zero CandidatePreview
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Catalog == nil || s.Source == nil || s.Objects == nil || s.Signer == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tid == uuid.Nil || id == uuid.Nil || (side != "before" && side != "after") {
		return zero, domain.NewError("INPUT_INVALID", 422)
	}
	profile, err := media.ProfileHash(variant, s.SRGBSHA)
	if err != nil {
		return zero, err
	}
	value, err := s.Catalog.Lookup(ctx, who, tid, id, side, variant)
	if err != nil {
		return zero, err
	}
	source, raw, err := s.Source.ReadCandidatePreviewSource(ctx, who, tid, id, side)
	if err != nil {
		return zero, err
	}
	if source != value.Source || !media.KnownProfileHash(value.ProfileHash,variant,s.SRGBSHA) || value.ManifestObject.SHA256 != value.Manifest.SHA256 || value.DerivativeObject.SHA256 != value.Derivative.SHA256 {
		return zero, objectstore.ErrIntegrity
	}
	if len(raw) < 2 || len(raw) > 1048576 {
		return zero, objectstore.ErrIntegrity
	}
	sourceBytes := append([]byte(nil), raw...)
	if derivativeManifestSHA(sourceBytes) != source.SHA256 {
		return zero, objectstore.ErrIntegrity
	}
	image, err := ParseWorkingImage(sourceBytes)
	if err != nil {
		return zero, err
	}
	manifestRaw, err := readObject(ctx, s.Objects, value.ManifestObject)
	if err != nil {
		return zero, err
	}
	manifest, err := ParseRenderDerivativeManifest(manifestRaw, value.Manifest, source, value.Derivative, image, variant, s.SRGBSHA)
	if err != nil {
		return zero, err
	}
	if manifest.ProfileHash!=value.ProfileHash {return zero,objectstore.ErrIntegrity}
	profile=manifest.ProfileHash
	if value.ManifestObject.ContentType != "application/json" || value.DerivativeObject.ContentType != manifest.MediaType || value.DerivativeObject.Bytes < 2 || value.DerivativeObject.Bytes > 52*1048576 {
		return zero, objectstore.ErrIntegrity
	}
	now := time.Now
	if s.Clock != nil {
		now = s.Clock
	}
	sign := func(ref ArtifactRef, object objectstore.Object) (CandidatePreviewLink, error) {
		if !ref.valid() || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != ref.SHA256 {
			return CandidatePreviewLink{}, objectstore.ErrIntegrity
		}
		link, err := s.Signer.SignRead(ctx, object, 5*time.Minute)
		if err != nil {
			return CandidatePreviewLink{}, err
		}
		if !objectstore.ValidSignedReadURL(link.URL, object.VersionID) || !link.ExpiresAt.After(now()) || link.ExpiresAt.After(now().Add(6*time.Minute)) {
			return CandidatePreviewLink{}, objectstore.ErrIntegrity
		}
		return CandidatePreviewLink{Artifact: ref, Bytes: object.Bytes, MediaType: object.ContentType, URL: link.URL, ExpiresAt: link.ExpiresAt}, nil
	}
	manifestLink, err := sign(value.Manifest, value.ManifestObject)
	if err != nil {
		return zero, err
	}
	imageLink, err := sign(value.Derivative, value.DerivativeObject)
	if err != nil {
		return zero, err
	}
	current, err := s.Catalog.Lookup(ctx, who, tid, id, side, variant)
	if err != nil {
		return zero, err
	}
	if current != value {
		return zero, objectstore.ErrIntegrity
	}
	currentSource, currentRaw, err := s.Source.ReadCandidatePreviewSource(ctx, who, tid, id, side)
	if err != nil {
		return zero, err
	}
	if currentSource != source || !bytes.Equal(currentRaw, sourceBytes) {
		return zero, objectstore.ErrIntegrity
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return CandidatePreview{Review: id, Side: side, Source: source, ProfileHash: profile, Width: manifest.Width, Height: manifest.Height, Manifest: manifestLink, Image: imageLink}, nil
}
