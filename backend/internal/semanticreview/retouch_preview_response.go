package semanticreview

import (
	"context"
	"photoeditor/backend/internal/objectstore"
	"time"
)

// BuildRetouchPreviewResponse exposes only physically verified media. The
// caller supplies a live authenticated authority check, repeated after signing.
func BuildRetouchPreviewResponse(ctx context.Context, physical PhysicalRetouchPreview,
	request PreviewRequest, now time.Time, sign PreviewURLSigner, checkAuthority func(context.Context) error) (Preview, error) {
	empty := Preview{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !physical.verified || sign == nil || checkAuthority == nil || now.IsZero() || request.Validate() != nil {
		return empty, ErrInvalid
	}
	linkage := physical.linkage
	p := linkage.Presentation.Params
	if p.WorkflowRevision != request.ExpectedRevision || p.TargetRevision != request.TargetRevision ||
		p.Plan.SHA256 != request.PlanSHA || p.Candidate.SHA256 != request.CandidateSHA {
		return empty, ErrConflict
	}
	if err := checkAuthority(ctx); err != nil {
		return empty, err
	}
	expiry := now.Add(time.Minute)
	media := [4]PreviewMedia{}
	for i := range media {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		source := linkage.Sidecars[i+3]
		address, err := sign(ctx, objectstore.Object{Key: source.Key, VersionID: source.Version,
			SHA256: source.Ref.SHA256, Bytes: source.Bytes, ContentType: source.MediaType}, expiry)
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if err != nil {
			return empty, err
		}
		if !objectstore.ValidSignedReadURL(address, source.Version) {
			return empty, ErrInvalid
		}
		media[i] = PreviewMedia{ArtifactID: source.Ref.ArtifactID, SHA256: source.Ref.SHA256,
			URL: address, ExpiresAt: expiry, Width: linkage.Presentation.Width, Height: linkage.Presentation.Height, MediaType: source.MediaType}
	}
	result := Preview{SchemaVersion: 1, TenantID: p.TenantID, IntentID: p.IntentID, TargetID: p.TargetID,
		TargetRevision: p.TargetRevision, AssetVersionID: p.Source.AssetVersionID, WorkflowID: p.WorkflowID,
		WorkflowRevision: p.WorkflowRevision, PlanSHA: p.Plan.SHA256,
		Candidate:     TargetDocument{ArtifactID: p.Candidate.ArtifactID, SHA256: p.Candidate.SHA256},
		Render:        TargetDocument{ArtifactID: linkage.Presentation.WorkingImage.ArtifactID, SHA256: linkage.Presentation.WorkingImage.SHA256},
		Evidence:      TargetDocument{ArtifactID: linkage.Document.Source.Ref.ArtifactID, SHA256: linkage.Document.Source.Ref.SHA256},
		QualityStatus: "review", Limitations: []string{"AI-assisted retouch_region; worker preservation evidence and artifact integrity verified. Photographic result requires photographer review."},
		ProtectedPixelsUnchanged: true, Before: media[0], After: media[1], Affected: media[2], Protected: media[3]}
	if result.Validate(p.TenantID, p.IntentID, p.TargetID, request, now) != nil {
		return empty, ErrInvalid
	}
	if err := checkAuthority(ctx); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
