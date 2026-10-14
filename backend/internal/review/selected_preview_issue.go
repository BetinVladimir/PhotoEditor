package review

import (
	"context"
	"net/url"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
)

// IssueSelectedRenderPreview substitutes per-request-authorized API links for
// direct S3 links. The API origin is validated by deployment configuration.
func (s CandidatePreviewService) IssueSelectedRenderPreview(ctx context.Context, who auth.Identity, tenant, reviewID uuid.UUID, variant string, key []byte, origin string) (CandidatePreview, error) {
	var zero CandidatePreview
	if len(key) < 32 || origin == "" || s.Selection == nil {
		return zero, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	preview, err := s.SelectedRenderPreview(ctx, who, tenant, reviewID, variant)
	if err != nil {
		return zero, err
	}
	selection, err := s.Selection.ReadRenderResultSelection(ctx, who, tenant, reviewID)
	if err != nil {
		return zero, err
	}
	return s.issueSelectedPreview(ctx, who, tenant, reviewID, variant, key, origin, "render", preview, selection)
}

func (s CandidatePreviewService) IssueSelectedRestorationPreview(ctx context.Context, who auth.Identity, tenant, reviewID uuid.UUID, variant string, key []byte, origin string) (CandidatePreview, error) {
	var zero CandidatePreview
	if len(key) < 32 || origin == "" || s.RestorationSelection == nil {
		return zero, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	preview, err := s.SelectedRestorationPreview(ctx, who, tenant, reviewID, variant)
	if err != nil {
		return zero, err
	}
	selection, err := s.RestorationSelection.ReadRestorationResultSelection(ctx, who, tenant, reviewID)
	if err != nil {
		return zero, err
	}
	return s.issueSelectedPreview(ctx, who, tenant, reviewID, variant, key, origin, "restoration", preview, selection)
}

func (s CandidatePreviewService) issueSelectedPreview(ctx context.Context, who auth.Identity, tenant, reviewID uuid.UUID, variant string, key []byte, origin, kind string, preview CandidatePreview, selection RenderResultSelection) (CandidatePreview, error) {
	var zero CandidatePreview
	if !validRenderPreviewSelection(selection, reviewID) || selection.Image == nil ||
		*selection.Image != preview.Source ||
		(selection.Route == "base" && preview.Side != "before") ||
		(selection.Route == "candidate" && preview.Side != "after") {
		return zero, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	side := preview.Side
	objects, err := s.Catalog.Lookup(context.WithValue(ctx, allowDecidedReviewContextKey{}, true), who, tenant, reviewID, side, variant)
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
		if !ok || link.Artifact.ID == uuid.Nil || link.ExpiresAt.Before(now().Add(time.Second)) ||
			!previewLinkObjectMatches(link, object, ref, version) {
			return CandidatePreviewLink{}, objectstore.ErrIntegrity
		}
		expiry := now().Add(2 * time.Minute)
		if link.ExpiresAt.Before(expiry) {
			expiry = link.ExpiresAt
		}
		grant := SelectedPreviewGrant{Version: 1, Tenant: tenant, Review: reviewID, Kind: kind,
			User: who.UserID, Session: who.SessionID, DecisionHash: selection.DecisionHash,
			Route: selection.Route, Variant: variant, Role: role, Artifact: link.Artifact.ID,
			SHA256: link.Artifact.SHA256, ObjectKeySHA: previewObjectKeySHA(object.Key),
			ObjectVersion: version, ExpiresUnix: expiry.Unix()}
		token, err := SealSelectedPreviewGrant(key, grant, now())
		if err != nil {
			return CandidatePreviewLink{}, err
		}
		path := "/api/v1/media/render-selected"
		if kind == "restoration" {
			path = "/api/v1/media/restoration-selected"
		}
		link.URL = origin + path + "?token=" + url.QueryEscape(token) + "&versionId=" + url.QueryEscape(version)
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
	var current RenderResultSelection
	if kind == "restoration" {
		if s.RestorationSelection == nil {
			return zero, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		current, err = s.RestorationSelection.ReadRestorationResultSelection(ctx, who, tenant, reviewID)
	} else if kind == "render" {
		if s.Selection == nil {
			return zero, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		current, err = s.Selection.ReadRenderResultSelection(ctx, who, tenant, reviewID)
	} else {
		return zero, domain.NewError("INPUT_INVALID", 422)
	}
	if err != nil {
		return zero, err
	}
	if !validRenderPreviewSelection(current, reviewID) || current.Image == nil ||
		current.DecisionHash != selection.DecisionHash || current.Route != selection.Route ||
		*current.Image != preview.Source {
		return zero, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	latest, err := s.Catalog.Lookup(context.WithValue(ctx, allowDecidedReviewContextKey{}, true), who, tenant, reviewID, side, variant)
	if err != nil {
		return zero, err
	}
	if latest != objects || !manifest.ExpiresAt.After(now()) || !image.ExpiresAt.After(now()) {
		return zero, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	preview.Manifest, preview.Image = manifest, image
	return preview, nil
}
