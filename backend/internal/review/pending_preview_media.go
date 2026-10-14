package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/url"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
)

// IssuePendingPreview preserves the existing physical preview verification
// while replacing both direct S3 links with per-GET-authorized API links.
func (s CandidatePreviewService) IssuePendingPreview(ctx context.Context, who auth.Identity, tenant, reviewID uuid.UUID, side, variant string, key []byte, origin string) (CandidatePreview, error) {
	var zero CandidatePreview
	if len(key) < 32 || origin == "" {
		return zero, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	preview, err := s.Preview(ctx, who, tenant, reviewID, side, variant)
	if err != nil {
		return zero, err
	}
	objects, err := s.Catalog.Lookup(ctx, who, tenant, reviewID, side, variant)
	if err != nil {
		return zero, err
	}
	if objects.Source != preview.Source || objects.ProfileHash != preview.ProfileHash {
		return zero, objectstore.ErrIntegrity
	}
	now := time.Now
	if s.Clock != nil {
		now = s.Clock
	}
	bind := func(role string, link CandidatePreviewLink, object objectstore.Object, ref ArtifactRef) (CandidatePreviewLink, error) {
		version, ok := objectstore.SignedReadVersion(link.URL)
		if !ok || !link.ExpiresAt.After(now().Add(time.Second)) ||
			!previewLinkObjectMatches(link, object, ref, version) {
			return CandidatePreviewLink{}, objectstore.ErrIntegrity
		}
		expiry := now().Add(2 * time.Minute)
		if link.ExpiresAt.Before(expiry) {
			expiry = link.ExpiresAt
		}
		grant := PendingPreviewGrant{Version: 1, Tenant: tenant, Review: reviewID,
			User: who.UserID, Session: who.SessionID, Side: side, Variant: variant,
			Source: preview.Source.ID, SourceSHA: preview.Source.SHA256,
			Role: role, Artifact: link.Artifact.ID, SHA256: link.Artifact.SHA256,
			ObjectKeySHA: previewObjectKeySHA(object.Key), ObjectVersion: version, ExpiresUnix: expiry.Unix()}
		token, err := SealPendingPreviewGrant(key, grant, now())
		if err != nil {
			return CandidatePreviewLink{}, err
		}
		link.URL = origin + "/api/v1/media/review-pending?token=" + url.QueryEscape(token) + "&versionId=" + url.QueryEscape(version)
		link.ExpiresAt = time.Unix(grant.ExpiresUnix, 0).UTC()
		return link, nil
	}
	manifest, err := bind("manifest", preview.Manifest, objects.ManifestObject, objects.Manifest)
	if err != nil {
		return zero, err
	}
	image, err := bind("image", preview.Image, objects.DerivativeObject, objects.Derivative)
	if err != nil {
		return zero, err
	}
	latest, err := s.Catalog.Lookup(ctx, who, tenant, reviewID, side, variant)
	if err != nil {
		return zero, err
	}
	if latest != objects {
		return zero, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	currentSource, currentRaw, err := s.Source.ReadCandidatePreviewSource(ctx, who, tenant, reviewID, side)
	if err != nil {
		return zero, err
	}
	if currentSource != preview.Source || len(currentRaw) < 2 || len(currentRaw) > 1048576 ||
		derivativeManifestSHA(currentRaw) != currentSource.SHA256 ||
		!manifest.ExpiresAt.After(now()) || !image.ExpiresAt.After(now()) {
		return zero, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	preview.Manifest, preview.Image = manifest, image
	return preview, nil
}

func (s CandidatePreviewService) ReadPendingPreview(ctx context.Context, key []byte, token string, now time.Time) ([]byte, string, error) {
	var empty []byte
	grant, err := OpenPendingPreviewGrant(key, token, now)
	if err != nil {
		return empty, "", domain.NewError("NOT_FOUND", 404)
	}
	if s.Catalog == nil || s.Source == nil || s.Objects == nil {
		return empty, "", domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	who := auth.Identity{UserID: grant.User, SessionID: grant.Session}
	lookup := func() (CandidatePreviewObjects, error) {
		catalogue, err := s.Catalog.Lookup(ctx, who, grant.Tenant, grant.Review, grant.Side, grant.Variant)
		if err != nil {
			return CandidatePreviewObjects{}, err
		}
		if catalogue.Source.ID != grant.Source || catalogue.Source.SHA256 != grant.SourceSHA {
			return CandidatePreviewObjects{}, objectstore.ErrIntegrity
		}
		ref, raw, err := s.Source.ReadCandidatePreviewSource(ctx, who, grant.Tenant, grant.Review, grant.Side)
		if err != nil {
			return CandidatePreviewObjects{}, err
		}
		if ref != catalogue.Source || len(raw) < 2 || len(raw) > 1048576 || derivativeManifestSHA(raw) != ref.SHA256 {
			return CandidatePreviewObjects{}, objectstore.ErrIntegrity
		}
		return catalogue, nil
	}
	first, err := lookup()
	if err != nil {
		return empty, "", err
	}
	ref, object, limit := first.Derivative, first.DerivativeObject, int64(52*1048576)
	if grant.Role == "manifest" {
		ref, object, limit = first.Manifest, first.ManifestObject, 1048576
	}
	if !pendingGrantObjectMatches(grant, ref, object, limit) {
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
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(raw)) != object.Bytes || int64(len(raw)) > limit {
		return empty, "", objectstore.ErrIntegrity
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != object.SHA256 {
		return empty, "", objectstore.ErrIntegrity
	}
	second, err := lookup()
	if err != nil {
		return empty, "", err
	}
	clock := time.Now
	if s.Clock != nil {
		clock = s.Clock
	}
	if second != first || clock().Unix() >= grant.ExpiresUnix {
		return empty, "", domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	if err := ctx.Err(); err != nil {
		return empty, "", err
	}
	return raw, object.ContentType, nil
}

func pendingGrantObjectMatches(grant PendingPreviewGrant, ref ArtifactRef, object objectstore.Object, max int64) bool {
	if ref.ID != grant.Artifact || ref.SHA256 != grant.SHA256 ||
		object.SHA256 != grant.SHA256 || object.VersionID != grant.ObjectVersion ||
		previewObjectKeySHA(object.Key) != grant.ObjectKeySHA ||
		object.Key == "" || object.Bytes < 2 || object.Bytes > max {
		return false
	}
	if grant.Role == "manifest" {
		return object.ContentType == "application/json"
	}
	return object.ContentType == "image/jpeg" || object.ContentType == "image/webp"
}
