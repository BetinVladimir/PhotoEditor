package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// BoundImportDefectReviewAuthority checks the exact prepared proposal around
// the independent, current-tenant decision authority. It never issues consent.
type BoundImportDefectReviewAuthority struct {
	Documents  ImportPreparedDocumentReader
	Candidates ImportDefectCandidatesQualifier
	Current    ImportDefectReviewAuthority
	Target     v2input.SessionVersionBinding
}

func (a BoundImportDefectReviewAuthority) validate(ctx context.Context, selection review.DefectSelection, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.Documents == nil || a.Candidates == nil || a.Current == nil || target != a.Target || !validImportRef(target.Source) {
		return v2input.ErrInvalid
	}
	if selection.Image.ID != target.Source.ArtifactID || selection.Image.SHA256 != target.Source.SHA256 {
		return v2input.ErrInvalid
	}
	entry := v2input.Entry{Ref: v2input.Ref{ArtifactID: selection.Proposal.ID, SHA256: selection.Proposal.SHA256}, Contract: "DefectCandidates"}
	raw, err := a.Documents.ReadPreparedImportDocument(ctx, entry, 1<<20)
	if err != nil {
		return err
	}
	proposal, err := review.ParseDefectProposalStructure(ctx, raw, selection.Proposal)
	if err != nil {
		return err
	}
	if err := a.Candidates.QualifyImportedDefectCandidates(ctx, raw, target); err != nil {
		return err
	}
	if err := proposal.ValidateSelection(selection); err != nil {
		return err
	}
	return ctx.Err()
}

func (a BoundImportDefectReviewAuthority) ReissueImportedDefectReview(ctx context.Context, archived, provisional review.DefectSelection, old, target v2input.SessionVersionBinding) (review.DefectSelection, error) {
	if a.Current == nil || target != a.Target || old.AssetVersionID == uuid.Nil || target.AssetVersionID == uuid.Nil ||
		!validImportRef(old.Source) || !validImportRef(target.Source) || !validImportRefForSHA(old.CanvasHash) ||
		old.AssetVersionID == target.AssetVersionID || old.Source.ArtifactID == target.Source.ArtifactID || old.CanvasHash != target.CanvasHash ||
		archived.Image.ID != old.Source.ArtifactID || archived.Image.SHA256 != old.Source.SHA256 ||
		provisional.Image.ID != target.Source.ArtifactID || provisional.Image.SHA256 != target.Source.SHA256 ||
		archived.Proposal.ID == provisional.Proposal.ID || archived.LabelMap.ID == provisional.LabelMap.ID {
		return review.DefectSelection{}, v2input.ErrInvalid
	}
	issued, err := a.Current.ReissueImportedDefectReview(ctx, archived, provisional, old, target)
	if err != nil {
		return review.DefectSelection{}, err
	}
	if err := a.validate(ctx, issued, target); err != nil {
		return review.DefectSelection{}, err
	}
	return issued, nil
}

func (a BoundImportDefectReviewAuthority) QualifyPreparedDefectReview(ctx context.Context, selection review.DefectSelection, target v2input.SessionVersionBinding) error {
	if err := a.validate(ctx, selection, target); err != nil {
		return err
	}
	if err := a.Current.QualifyPreparedDefectReview(ctx, selection, target); err != nil {
		return err
	}
	return a.validate(ctx, selection, target)
}

var _ ImportDefectReviewAuthority = BoundImportDefectReviewAuthority{}
