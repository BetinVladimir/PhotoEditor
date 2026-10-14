package interchange

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type ImportCurrentDecisionReader interface {
	Read(context.Context, uuid.UUID, v2input.SessionVersionBinding, string, v2input.Ref, v2input.Ref, *v2input.Ref) (ImportLiveReviewDecision, error)
}

// The current grant replaces archived selections. Additional operator policy
// may veto the resulting selection but cannot issue consent or change IDs.
type SQLImportDefectReviewIssuer struct {
	Entry      uuid.UUID
	Target     v2input.SessionVersionBinding
	Decisions  ImportCurrentDecisionReader
	Additional ImportDefectReviewAuthority
}

func (a SQLImportDefectReviewIssuer) issue(ctx context.Context, provisional review.DefectSelection, target v2input.SessionVersionBinding) (review.DefectSelection, error) {
	if err := ctx.Err(); err != nil {
		return review.DefectSelection{}, err
	}
	if a.Decisions == nil || a.Entry == uuid.Nil || target != a.Target || provisional.Image.ID != target.Source.ArtifactID || provisional.Image.SHA256 != target.Source.SHA256 {
		return review.DefectSelection{}, v2input.ErrInvalid
	}
	label := v2input.Ref{ArtifactID: provisional.LabelMap.ID, SHA256: provisional.LabelMap.SHA256}
	decision, err := a.Decisions.Read(ctx, a.Entry, target, "defects", v2input.Ref{ArtifactID: provisional.Proposal.ID, SHA256: provisional.Proposal.SHA256}, target.Source, &label)
	if err != nil {
		return review.DefectSelection{}, err
	}
	issued := provisional
	issued.Revision = decision.Epoch
	issued.Accepted = append([]string{}, decision.Accepted...)
	issued.Rejected = append([]string{}, decision.Rejected...)
	if review.ValidateDefectSelectionStructure(issued) != nil {
		return review.DefectSelection{}, v2input.ErrInvalid
	}
	return issued, ctx.Err()
}

func (a SQLImportDefectReviewIssuer) ReissueImportedDefectReview(ctx context.Context, archived, provisional review.DefectSelection, old, target v2input.SessionVersionBinding) (review.DefectSelection, error) {
	issued, err := a.issue(ctx, provisional, target)
	if err != nil {
		return review.DefectSelection{}, err
	}
	if a.Additional != nil {
		checked, err := a.Additional.ReissueImportedDefectReview(ctx, archived, issued, old, target)
		if err != nil {
			return review.DefectSelection{}, err
		}
		if checked.Version != issued.Version || checked.Image != issued.Image || checked.Proposal != issued.Proposal || checked.LabelMap != issued.LabelMap ||
			checked.Revision != issued.Revision || !slices.Equal(checked.Accepted, issued.Accepted) || !slices.Equal(checked.Rejected, issued.Rejected) {
			return review.DefectSelection{}, v2input.ErrInvalid
		}
	}
	return issued, ctx.Err()
}

func (a SQLImportDefectReviewIssuer) QualifyPreparedDefectReview(ctx context.Context, selection review.DefectSelection, target v2input.SessionVersionBinding) error {
	issued, err := a.issue(ctx, selection, target)
	if err != nil {
		return err
	}
	if issued.Revision != selection.Revision || !slices.Equal(issued.Accepted, selection.Accepted) || !slices.Equal(issued.Rejected, selection.Rejected) {
		return v2input.ErrInvalid
	}
	if a.Additional != nil {
		if err := a.Additional.QualifyPreparedDefectReview(ctx, selection, target); err != nil {
			return err
		}
	}
	current, err := a.issue(ctx, selection, target)
	if err != nil {
		return err
	}
	if current.Revision != selection.Revision || !slices.Equal(current.Accepted, selection.Accepted) || !slices.Equal(current.Rejected, selection.Rejected) {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

type SQLImportDustReviewIssuer struct {
	Entry      uuid.UUID
	Target     v2input.SessionVersionBinding
	Decisions  ImportCurrentDecisionReader
	Additional ImportDustReviewAuthority
}

func (a SQLImportDustReviewIssuer) issue(ctx context.Context, provisional review.DustSelection, target v2input.SessionVersionBinding) (review.DustSelection, error) {
	if err := ctx.Err(); err != nil {
		return review.DustSelection{}, err
	}
	if a.Decisions == nil || a.Entry == uuid.Nil || target != a.Target || provisional.Base.ID != target.Source.ArtifactID || provisional.Base.SHA256 != target.Source.SHA256 {
		return review.DustSelection{}, v2input.ErrInvalid
	}
	decision, err := a.Decisions.Read(ctx, a.Entry, target, "dust", v2input.Ref{ArtifactID: provisional.Proposal.ID, SHA256: provisional.Proposal.SHA256}, target.Source, nil)
	if err != nil {
		return review.DustSelection{}, err
	}
	issued := provisional
	issued.Revision = decision.Epoch
	issued.Accepted = append([]string{}, decision.Accepted...)
	issued.Rejected = append([]string{}, decision.Rejected...)
	if review.ValidateDustSelectionStructure(issued) != nil {
		return review.DustSelection{}, v2input.ErrInvalid
	}
	return issued, ctx.Err()
}

func (a SQLImportDustReviewIssuer) ReissueImportedDustReview(ctx context.Context, archived, provisional review.DustSelection, old, target v2input.SessionVersionBinding) (review.DustSelection, error) {
	issued, err := a.issue(ctx, provisional, target)
	if err != nil {
		return review.DustSelection{}, err
	}
	if a.Additional != nil {
		checked, err := a.Additional.ReissueImportedDustReview(ctx, archived, issued, old, target)
		if err != nil {
			return review.DustSelection{}, err
		}
		if checked.Version != issued.Version || checked.Proposal != issued.Proposal || checked.Base != issued.Base || checked.Revision != issued.Revision ||
			!slices.Equal(checked.Accepted, issued.Accepted) || !slices.Equal(checked.Rejected, issued.Rejected) {
			return review.DustSelection{}, v2input.ErrInvalid
		}
	}
	return issued, ctx.Err()
}

func (a SQLImportDustReviewIssuer) QualifyPreparedDustReview(ctx context.Context, selection review.DustSelection, target v2input.SessionVersionBinding) error {
	issued, err := a.issue(ctx, selection, target)
	if err != nil {
		return err
	}
	if issued.Revision != selection.Revision || !slices.Equal(issued.Accepted, selection.Accepted) || !slices.Equal(issued.Rejected, selection.Rejected) {
		return v2input.ErrInvalid
	}
	if a.Additional != nil {
		if err := a.Additional.QualifyPreparedDustReview(ctx, selection, target); err != nil {
			return err
		}
	}
	current, err := a.issue(ctx, selection, target)
	if err != nil {
		return err
	}
	if current.Revision != selection.Revision || !slices.Equal(current.Accepted, selection.Accepted) || !slices.Equal(current.Rejected, selection.Rejected) {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ImportDefectReviewAuthority = SQLImportDefectReviewIssuer{}
var _ ImportDustReviewAuthority = SQLImportDustReviewIssuer{}
