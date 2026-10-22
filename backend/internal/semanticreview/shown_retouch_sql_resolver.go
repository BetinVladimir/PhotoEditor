package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"strconv"
)

// Validate the complete selection before any report or physical preview reads.
// Every approved target needs exactly one copy budget; rejected targets must
// never acquire a budget or consume physical preparation work.
func shownRetouchApplyBudgets(request ApplyDecision, budgets []ExposureInputBudget) (map[uuid.UUID]ExposureInputBudget, error) {
	approved := make(map[uuid.UUID]bool)
	for _, decision := range request.TargetDecisions {
		if decision.Decision == "approve" {
			approved[decision.TargetID] = true
		}
	}
	if len(approved) == 0 || len(approved) != len(budgets) {
		return nil, ErrInvalid
	}
	result := make(map[uuid.UUID]ExposureInputBudget, len(budgets))
	for _, item := range budgets {
		if _, duplicate := result[item.TargetID]; duplicate || item.TargetID == uuid.Nil || !approved[item.TargetID] ||
			item.Limits.VRAMBytes != 0 || !validRetouchQualification(RetouchQualificationPin(item.Qualification)) {
			return nil, ErrInvalid
		}
		result[item.TargetID] = item
	}
	return result, nil
}

// NewSQLShownRetouchApplyResolver composes authenticated proposal reads and full
// physical preview verification. Budgets must describe the copy operation.
func NewSQLShownRetouchApplyResolver(db *pgxpool.Pool, preview *RetouchPreviewProvider, read PlanObjectReader,
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
		if review.Intent.Kind != "retouch_region" {
			return empty, ErrConflict
		}
		budgets, err := selectBudgets(ctx, who, tenant, intent, owned)
		if err != nil {
			return empty, err
		}
		owned, err = DecodeApplyDecision(raw)
		if err != nil {
			return empty, err
		}
		byBudget, err := shownRetouchApplyBudgets(owned, budgets)
		if err != nil {
			return empty, err
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
			if err = VerifyShownRetouchReviewReport(ctx, db, read, preview.bucket, tenant, intent, who.UserID, who.SessionID, review.Sources.Revision, RetouchQualificationPin(costs.Qualification)); err != nil {
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
			if err := validateShownRetouchSelection(decision, fence, selected, physical); err != nil {
				return empty, err
			}
			p := physical.linkage.Presentation.Params
			scoped := ScopedPreviewReview{Review: review, Target: target, Workflow: p.WorkflowID}
			source, _, err := LoadShownRetouchSourceDocuments(ctx, db, who, tenant, intent, scoped, fence, read, costs.OwnerCosts, costs.Limits, RetouchQualificationPin(costs.Qualification), p)
			if err != nil {
				return empty, err
			}
			var intentRaw []byte
			for _, doc := range review.documents {
				if doc.Source.Ref == p.Intent {
					intentRaw = append([]byte(nil), doc.Body...)
				}
			}
			result.RetouchInputs = append(result.RetouchInputs, ShownRetouchPreparationInput{Selected: selected, Physical: physical, Candidate: candidateRaw, Intent: intentRaw,
				Documents: source.Documents, Owner: costs.OwnerCosts, Limits: costs.Limits, Qualification: RetouchQualificationPin(costs.Qualification)})
			fences[target.ID] = fence
		}
		if len(result.RetouchInputs) == 0 || len(result.RetouchInputs) != len(budgets) {
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
