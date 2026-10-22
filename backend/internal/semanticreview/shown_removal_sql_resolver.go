package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"strconv"
)

// NewSQLShownRemovalApplyResolver composes authenticated proposal reads and full
// physical preview verification. Budgets must describe the copy operation.
func NewSQLShownRemovalApplyResolver(db *pgxpool.Pool, preview *RemovalPreviewProvider, read PlanObjectReader,
	budget int64, modelSHA, datasetSHA string, selectBudgets ExposureBudgetSelection) (ShownApplyResolver, error) {
	if db == nil || preview == nil || read == nil || selectBudgets == nil || budget < 1 || budget > 64*1048576 ||
		!shaPattern.MatchString(modelSHA) || !shaPattern.MatchString(datasetSHA) {
		return nil, ErrInvalid
	}
	return func(ctx context.Context, who auth.Identity, tenant, intent, child uuid.UUID, request ApplyDecision) (ShownApplyResolution, error) {
		empty := ShownApplyResolution{}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if tenant == uuid.Nil || intent == uuid.Nil || child == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
			return empty, ErrInvalid
		}
		raw, err := canonical.Marshal(request)
		if err != nil {
			return empty, ErrInvalid
		}
		owned, err := DecodeApplyDecision(raw)
		if err != nil {
			return empty, err
		}
		review, targets, err := PrepareScopedApplyReview(ctx, db, who, tenant, intent, owned, read, budget, modelSHA, datasetSHA)
		if err != nil {
			return empty, err
		}
		budgets, err := selectBudgets(ctx, who, tenant, intent, owned)
		if err != nil {
			return empty, err
		}
		owned, err = DecodeApplyDecision(raw)
		if err != nil {
			return empty, err
		}
		byBudget := map[uuid.UUID]ExposureInputBudget{}
		for _, item := range budgets {
			if _, ok := byBudget[item.TargetID]; ok {
				return empty, ErrInvalid
			}
			byBudget[item.TargetID] = item
		}
		byTarget := map[uuid.UUID]LockedTarget{}
		for _, target := range targets {
			byTarget[target.ID] = target
		}
		fences := map[uuid.UUID]PreviewRequest{}
		result := ShownApplyResolution{Review: review, Targets: targets}
		for _, decision := range owned.TargetDecisions {
			if decision.Decision != "approve" {
				continue
			}
			target, ok := byTarget[decision.TargetID]
			if !ok || target.CandidateArtifactID == nil || decision.Preview == nil {
				return empty, ErrInvalid
			}
			costs, ok := byBudget[target.ID]
			if !ok {
				return empty, ErrInvalid
			}
			if err = VerifyShownRemovalReviewReport(ctx, db, read, preview.bucket, tenant, intent, who.UserID, who.SessionID, review.Sources.Revision, costs.Qualification); err != nil {
				return empty, err
			}
			var candidateRaw []byte
			candidateSHA := ""
			for _, doc := range review.documents {
				if doc.Source.Ref.ArtifactID == *target.CandidateArtifactID && doc.Source.Contract == "TransferCandidate" {
					candidateRaw = append([]byte(nil), doc.Body...)
					candidateSHA = doc.Source.Ref.SHA256
				}
			}
			fence := PreviewRequest{ExpectedRevision: owned.ExpectedRevision, TargetRevision: strconv.FormatInt(target.Revision, 10), PlanSHA: owned.PlanSHA, CandidateSHA: candidateSHA}
			if fence.Validate() != nil {
				return empty, ErrInvalid
			}
			physical, selected, err := preview.ReadPhysicalPreview(ctx, who, tenant, intent, target.ID, fence)
			if err != nil {
				return empty, err
			}
			if decision.Preview.Render.ArtifactID != selected.Receipt.WorkingImage.ArtifactID || decision.Preview.Render.SHA256 != selected.Receipt.WorkingImage.SHA256 ||
				decision.Preview.Evidence.ArtifactID != selected.Receipt.Presentation.ArtifactID || decision.Preview.Evidence.SHA256 != selected.Receipt.Presentation.SHA256 {
				return empty, ErrConflict
			}
			p := physical.linkage.Presentation.Params
			scoped := ScopedPreviewReview{Review: review, Target: target, Workflow: p.WorkflowID}
			source, _, err := LoadShownRemovalSourceDocuments(ctx, db, who, tenant, intent, scoped, fence, read, costs.OwnerCosts, costs.Limits, costs.Qualification, p)
			if err != nil {
				return empty, err
			}
			var intentRaw []byte
			for _, doc := range review.documents {
				if doc.Source.Ref == p.Intent {
					intentRaw = append([]byte(nil), doc.Body...)
				}
			}
			result.RemovalInputs = append(result.RemovalInputs, ShownRemovalPreparationInput{Selected: selected, Physical: physical, Candidate: candidateRaw, Intent: intentRaw,
				Documents: source.Documents, Owner: costs.OwnerCosts, Limits: costs.Limits, Qualification: costs.Qualification})
			fences[target.ID] = fence
		}
		if len(result.RemovalInputs) == 0 || len(result.RemovalInputs) != len(budgets) {
			return empty, ErrInvalid
		}
		result.Recheck = func(ctx context.Context) error {
			for _, decision := range owned.TargetDecisions {
				if fence, ok := fences[decision.TargetID]; ok {
					if err := CheckCurrentPreviewAuthority(ctx, db, who, tenant, intent, decision.TargetID, fence); err != nil {
						return err
					}
				}
			}
			return ctx.Err()
		}
		if err = result.Recheck(ctx); err != nil {
			return empty, err
		}
		return result, nil
	}, nil
}
