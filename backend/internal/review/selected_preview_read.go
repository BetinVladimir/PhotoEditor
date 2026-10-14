package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
)

// ReadSelectedRenderPreview verifies a render or restoration capability against live SQL authority
// before and after bounded physical IO. The caller must not cache the returned
// bytes and must send no body before this method has completed successfully.
func (s CandidatePreviewService) ReadSelectedRenderPreview(ctx context.Context, key []byte, token string, now time.Time) ([]byte, string, error) {
	var empty []byte
	grant, err := OpenSelectedPreviewGrant(key, token, now)
	if err != nil {
		return empty, "", domain.NewError("NOT_FOUND", 404)
	}
	if s.Catalog == nil || s.Source == nil || s.Objects == nil ||
		(grant.Kind == "render" && s.Selection == nil) ||
		(grant.Kind == "restoration" && s.RestorationSelection == nil) {
		return empty, "", domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	who := auth.Identity{UserID: grant.User, SessionID: grant.Session}
	readSelection := func() (RenderResultSelection, error) {
		if grant.Kind == "restoration" {
			return s.RestorationSelection.ReadRestorationResultSelection(ctx, who, grant.Tenant, grant.Review)
		}
		return s.Selection.ReadRenderResultSelection(ctx, who, grant.Tenant, grant.Review)
	}
	selection, err := readSelection()
	if err != nil {
		return empty, "", err
	}
	if !selectedGrantMatches(selection, grant) {
		return empty, "", domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	side := "after"
	if grant.Route == "base" {
		side = "before"
	}
	selectedCtx := context.WithValue(ctx, allowDecidedReviewContextKey{}, true)
	objects, err := s.Catalog.Lookup(selectedCtx, who, grant.Tenant, grant.Review, side, grant.Variant)
	if err != nil {
		return empty, "", err
	}
	if objects.Source != *selection.Image {
		return empty, "", objectstore.ErrIntegrity
	}
	readSource := func() error {
		ref, body, err := s.Source.ReadCandidatePreviewSource(selectedCtx, who, grant.Tenant, grant.Review, side)
		if err != nil {
			return err
		}
		if ref != objects.Source || len(body) < 2 || len(body) > 1048576 ||
			derivativeManifestSHA(body) != ref.SHA256 {
			return objectstore.ErrIntegrity
		}
		return nil
	}
	if err := readSource(); err != nil {
		return empty, "", err
	}
	ref, object := objects.Derivative, objects.DerivativeObject
	maxBytes := int64(52 * 1048576)
	if grant.Role == "manifest" {
		ref, object, maxBytes = objects.Manifest, objects.ManifestObject, 1048576
	}
	if !selectedGrantObjectMatches(grant, ref, object, maxBytes) {
		return empty, "", objectstore.ErrIntegrity
	}
	release, err := acquireReviewMediaSlot(ctx, reviewMediaSlots)
	if err != nil {
		return empty, "", err
	}
	defer release()
	reader, err := s.Objects.Open(ctx, object)
	if err != nil {
		return empty, "", err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil || int64(len(raw)) != object.Bytes || int64(len(raw)) > maxBytes {
		return empty, "", objectstore.ErrIntegrity
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != object.SHA256 {
		return empty, "", objectstore.ErrIntegrity
	}
	if err := ctx.Err(); err != nil {
		return empty, "", err
	}
	current, err := readSelection()
	if err != nil {
		return empty, "", err
	}
	if !selectedGrantMatches(current, grant) {
		return empty, "", domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	if err := readSource(); err != nil {
		return empty, "", err
	}
	latest, err := s.Catalog.Lookup(selectedCtx, who, grant.Tenant, grant.Review, side, grant.Variant)
	if err != nil {
		return empty, "", err
	}
	clock := time.Now
	if s.Clock != nil {
		clock = s.Clock
	}
	if latest != objects || clock().Unix() >= grant.ExpiresUnix {
		return empty, "", domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	if err := ctx.Err(); err != nil {
		return empty, "", err
	}
	return raw, object.ContentType, nil
}

func selectedGrantMatches(selection RenderResultSelection, grant SelectedPreviewGrant) bool {
	return validRenderPreviewSelection(selection, grant.Review) &&
		selection.Route == grant.Route && selection.DecisionHash == grant.DecisionHash &&
		selection.Image != nil
}

func selectedGrantObjectMatches(grant SelectedPreviewGrant, ref ArtifactRef, object objectstore.Object, maxBytes int64) bool {
	if ref.ID != grant.Artifact || ref.SHA256 != grant.SHA256 ||
		object.SHA256 != grant.SHA256 || object.VersionID != grant.ObjectVersion ||
		previewObjectKeySHA(object.Key) != grant.ObjectKeySHA ||
		object.Bytes < 2 || object.Bytes > maxBytes || object.Key == "" {
		return false
	}
	if grant.Role == "manifest" {
		return object.ContentType == "application/json"
	}
	return object.ContentType == "image/jpeg" || object.ContentType == "image/webp"
}
