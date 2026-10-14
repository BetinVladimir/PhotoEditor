package interchange

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// BoundImportDustReviewAuthority checks the exact prepared series and every
// issued candidate ID. Current remains the independent tenant decision issuer.
type BoundImportDustReviewAuthority struct {
	Documents ImportPreparedDocumentReader
	Series    ImportDustSeriesAuthority
	Current   ImportDustReviewAuthority
	Target    v2input.SessionVersionBinding
}

func (a BoundImportDustReviewAuthority) validate(ctx context.Context, selection review.DustSelection, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.Documents == nil || a.Series == nil || a.Current == nil || target != a.Target || !validImportRef(target.Source) || selection.Base.ID != target.Source.ArtifactID || selection.Base.SHA256 != target.Source.SHA256 {
		return v2input.ErrInvalid
	}
	entry := v2input.Entry{Ref: v2input.Ref{ArtifactID: selection.Proposal.ID, SHA256: selection.Proposal.SHA256}, Contract: "DustCandidates"}
	raw, err := a.Documents.ReadPreparedImportDocument(ctx, entry, 1<<20)
	if err != nil {
		return err
	}
	proposal, err := review.ReadDustProposal(ctx, bytes.NewReader(raw), selection.Proposal, selection.Base)
	if err != nil {
		return err
	}
	if err := a.Series.QualifyPreparedDustSeries(ctx, raw, target); err != nil {
		return err
	}
	if err := proposal.ValidateSelection(selection); err != nil {
		return err
	}
	return ctx.Err()
}

func (a BoundImportDustReviewAuthority) ReissueImportedDustReview(ctx context.Context, archived, provisional review.DustSelection, old, target v2input.SessionVersionBinding) (review.DustSelection, error) {
	if a.Current == nil || target != a.Target || old.AssetVersionID == uuid.Nil || target.AssetVersionID == uuid.Nil ||
		!validImportRef(old.Source) || !validImportRef(target.Source) || !validImportRefForSHA(old.CanvasHash) ||
		old.AssetVersionID == target.AssetVersionID || old.Source.ArtifactID == target.Source.ArtifactID || old.CanvasHash != target.CanvasHash ||
		archived.Base.ID != old.Source.ArtifactID || archived.Base.SHA256 != old.Source.SHA256 ||
		provisional.Base.ID != target.Source.ArtifactID || provisional.Base.SHA256 != target.Source.SHA256 ||
		archived.Proposal.ID == provisional.Proposal.ID {
		return review.DustSelection{}, v2input.ErrInvalid
	}
	issued, err := a.Current.ReissueImportedDustReview(ctx, archived, provisional, old, target)
	if err != nil {
		return review.DustSelection{}, err
	}
	if err := a.validate(ctx, issued, target); err != nil {
		return review.DustSelection{}, err
	}
	return issued, nil
}

func (a BoundImportDustReviewAuthority) QualifyPreparedDustReview(ctx context.Context, selection review.DustSelection, target v2input.SessionVersionBinding) error {
	if err := a.validate(ctx, selection, target); err != nil {
		return err
	}
	if err := a.Current.QualifyPreparedDustReview(ctx, selection, target); err != nil {
		return err
	}
	return a.validate(ctx, selection, target)
}

var _ ImportDustReviewAuthority = BoundImportDustReviewAuthority{}
