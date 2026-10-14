package interchange

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// The series authority must prove every mapped frame belongs to the same
// physical camera and the target import. A single entry's resolver only knows
// its own WorkingImage and cannot authorize the other frames in this series.
type ImportDustSeriesAuthority interface {
	ValidateArchivedDustSeries(context.Context, []byte, v2input.SessionVersionBinding) error
	MapDustSeriesFrame(context.Context, review.ArtifactRef, v2input.SessionVersionBinding, v2input.SessionVersionBinding) (review.ArtifactRef, error)
	QualifyPreparedDustSeries(context.Context, []byte, v2input.SessionVersionBinding) error
}

type ImportDustCandidatesCodec struct{ Authority ImportDustSeriesAuthority }

func (c ImportDustCandidatesCodec) RemapImportDocument(ctx context.Context, kind string, raw []byte, old, target v2input.SessionVersionBinding, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if kind != "DustCandidates" {
		return nil, &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if c.Authority == nil || resolve == nil || !validImportRef(old.Source) || !validImportRef(target.Source) || old.AssetVersionID == uuid.Nil || target.AssetVersionID == uuid.Nil || old.AssetVersionID == target.AssetVersionID || old.CanvasHash != target.CanvasHash {
		return nil, &ImportRecipeUnsupported{Reason: "dust_series_authority_not_installed"}
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, v2input.ErrInvalid
	}
	if err := c.Authority.ValidateArchivedDustSeries(ctx, append([]byte(nil), raw...), old); err != nil {
		return nil, err
	}
	original := review.ArtifactRef{ID: old.Source.ArtifactID, SHA256: old.Source.SHA256}
	current := review.ArtifactRef{ID: target.Source.ArtifactID, SHA256: target.Source.SHA256}
	prepared, _, err := review.RemapDustProposalFrames(ctx, raw, original, current, func(ctx context.Context, frame review.ArtifactRef) (review.ArtifactRef, error) {
		if frame == original {
			mapped, err := resolve(old.Source, "WorkingImage")
			if err != nil {
				return review.ArtifactRef{}, err
			}
			if mapped != target.Source {
				return review.ArtifactRef{}, v2input.ErrInvalid
			}
			return current, nil
		}
		return c.Authority.MapDustSeriesFrame(ctx, frame, old, target)
	})
	if err != nil {
		return nil, err
	}
	if err := c.QualifyRemappedImportDocument(ctx, kind, prepared, target); err != nil {
		return nil, err
	}
	return prepared, ctx.Err()
}

func (c ImportDustCandidatesCodec) QualifyRemappedImportDocument(ctx context.Context, kind string, raw []byte, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if kind != "DustCandidates" {
		return &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if c.Authority == nil || !validImportRef(target.Source) {
		return &ImportRecipeUnsupported{Reason: "dust_series_authority_not_installed"}
	}
	base := review.ArtifactRef{ID: target.Source.ArtifactID, SHA256: target.Source.SHA256}
	if _, err := review.ReadDustProposal(ctx, bytes.NewReader(raw), review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(raw)}, base); err != nil {
		return err
	}
	return c.Authority.QualifyPreparedDustSeries(ctx, append([]byte(nil), raw...), target)
}
