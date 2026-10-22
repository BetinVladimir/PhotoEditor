package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

// LockApplyRetouchPreviewSelections is for new mutations only, after idempotency replay.
// Caller owns the transaction and must roll back errors. No object IO occurs here.
func LockApplyRetouchPreviewSelections(ctx context.Context, tx pgx.Tx, tenant, intent, actor, session uuid.UUID,
	prepared PreparedReview, targets []LockedTarget, request ApplyDecision) ([]LockedRetouchPreviewSelection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return nil, ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return nil, err
	}
	owned, err := DecodeApplyDecision(raw)
	if err != nil {
		return nil, err
	}
	review, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA)
	if err != nil {
		return nil, err
	}
	states := map[uuid.UUID]string{}
	for _, target := range targets {
		states[target.ID] = target.State
	}
	decisions, err := ValidateSelection(owned, review.PlanRef.SHA256, strconv.FormatInt(review.Sources.Revision, 10), states)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]LockedTarget{}
	for _, target := range targets {
		if _, exists := byID[target.ID]; exists {
			return nil, ErrInvalid
		}
		byID[target.ID] = target
	}
	result := []LockedRetouchPreviewSelection{}
	for _, decision := range decisions {
		if decision.Decision != "approve" {
			continue
		}
		if decision.Preview == nil {
			return nil, ErrInvalid
		}
		target := byID[decision.TargetID]
		var candidateSHA string
		for _, doc := range review.documents {
			if target.CandidateArtifactID != nil && doc.Source.Contract == "TransferCandidate" && doc.Source.Ref.ArtifactID == *target.CandidateArtifactID {
				candidateSHA = doc.Source.Ref.SHA256
			}
		}
		if !shaPattern.MatchString(candidateSHA) {
			return nil, ErrInvalid
		}
		var run uuid.UUID
		var rawReceipt []byte
		err := tx.QueryRow(ctx, "SELECT preview_id,receipt FROM photo.lock_semantic_retouch_preview_selection($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)",
			tenant, intent, actor, session, review.Sources.Revision, target.ID, target.Revision, owned.PlanSHA, candidateSHA,
			decision.Preview.Render.ArtifactID, decision.Preview.Render.SHA256, decision.Preview.Evidence.ArtifactID, decision.Preview.Evidence.SHA256).Scan(&run, &rawReceipt)
		if err != nil {
			return nil, err
		}
		var receipt RetouchPreviewReceipt
		if len(rawReceipt) < 2 || len(rawReceipt) > 16384 || decodeStrict(rawReceipt, &receipt) != nil || run == uuid.Nil ||
			receipt.SchemaVersion != 1 || receipt.TaskID != uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+target.ID.String())) ||
			receipt.ResultID == uuid.Nil || receipt.Attempt < 1 || receipt.Fence < 1 || !shaPattern.MatchString(receipt.ParamsSHA) ||
			receipt.Presentation.ArtifactID != decision.Preview.Evidence.ArtifactID || receipt.Presentation.SHA256 != decision.Preview.Evidence.SHA256 ||
			receipt.WorkingImage.ArtifactID != decision.Preview.Render.ArtifactID || receipt.WorkingImage.SHA256 != decision.Preview.Render.SHA256 {
			return nil, ErrInvalid
		}
		seen := map[uuid.UUID]bool{}
		for _, ref := range []v2input.Ref{receipt.Presentation, receipt.WorkingImage, receipt.Pixels, receipt.NumericalEvidence} {
			if ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) || seen[ref.ArtifactID] {
				return nil, ErrInvalid
			}
			seen[ref.ArtifactID] = true
		}
		result = append(result, LockedRetouchPreviewSelection{TargetID: target.ID, PreviewRunID: run, Receipt: receipt})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
