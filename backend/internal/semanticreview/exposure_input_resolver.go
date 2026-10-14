package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

// ExposureInputBudget carries qualification and measured owner/resource costs;
// source dimensions, refs and protection pins are derived under SQL authority.
type ExposureInputBudget struct {
	TargetID      uuid.UUID
	OwnerCosts    ExposureOwnerCosts
	Limits        ExposureResourceEstimate
	Qualification ExposureQualificationPin
}

// ReadApprovedExposureInputBatch performs authenticated document, physical NPY
// and original proposal geometry checks for every approval, with no partial
// result. It is preflight only: scientific qualification, protected overlap,
// live final scope and atomic admission remain mandatory coordinator duties.
func ReadApprovedExposureInputBatch(ctx context.Context, db ApprovedProtectionQueryer, objects planObjectStore, bucket string,
	tenant, intent, actor, session, child uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision,
	budgets []ExposureInputBudget, color ExposureColorProfile) ([]ExposureTaskInputs, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !color.valid() || db == nil || objects == nil || bucket == "" || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || child == uuid.Nil || child == prepared.Sources.RunID {
		return nil, ErrInvalid
	}
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil {
		return nil, err
	}
	if len(budgets) != len(edits) {
		return nil, ErrInvalid
	}
	costs := make(map[uuid.UUID]ExposureInputBudget, len(budgets))
	for _, budget := range budgets {
		if budget.TargetID == uuid.Nil {
			return nil, ErrInvalid
		}
		if _, exists := costs[budget.TargetID]; exists {
			return nil, ErrInvalid
		}
		costs[budget.TargetID] = budget
	}
	for _, edit := range edits {
		if edit.Kind != "local_exposure" {
			return nil, ErrInvalid
		}
		if _, exists := costs[edit.TargetID]; !exists {
			return nil, ErrInvalid
		}
	}
	if len(edits) == 0 {
		return []ExposureTaskInputs{}, nil
	}
	pins, err := ReadApprovedProtectionPins(ctx, db, tenant, intent, actor, session, prepared, targets, request)
	if err != nil {
		return nil, err
	}
	read, err := NewPlanObjectReader(objects, bucket)
	if err != nil {
		return nil, err
	}
	documents := make([]ExposureTargetDocuments, 0, len(edits))
	for _, edit := range edits {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source, err := ReadApprovedImageDocument(ctx, db, tenant, intent, actor, session, prepared, targets, request, edit.TargetID, false, read)
		if err != nil {
			return nil, err
		}
		reference, err := ReadApprovedImageDocument(ctx, db, tenant, intent, actor, session, prepared, targets, request, edit.TargetID, true, read)
		if err != nil {
			return nil, err
		}
		if err := color.VerifyWorkingImage(source.Body); err != nil {
			return nil, err
		}
		if err := color.VerifyWorkingImage(reference.Body); err != nil {
			return nil, err
		}
		mask, err := ReadApprovedCandidateDocument(ctx, db, tenant, intent, actor, session, prepared, targets, request, edit.TargetID, false, read)
		if err != nil {
			return nil, err
		}
		transform, err := ReadApprovedCandidateDocument(ctx, db, tenant, intent, actor, session, prepared, targets, request, edit.TargetID, true, read)
		if err != nil {
			return nil, err
		}
		protection, err := ReadApprovedProtectionDocument(ctx, db, tenant, intent, actor, session, prepared, targets, request, edit.TargetID, pins[edit.TargetID], read)
		if err != nil {
			return nil, err
		}
		if err := ValidateApprovedExposureGeometry(prepared, targets, request, edit.TargetID, source.Body, reference.Body, mask.Body, transform.Body); err != nil {
			return nil, err
		}
		if _, err := VerifyApprovedExposureMaskSeparation(ctx, db, objects, bucket, tenant, intent, actor, session, prepared, targets, request, edit.TargetID, mask, protection); err != nil {
			return nil, err
		}
		for _, entry := range []struct {
			doc       PlanDocument
			reference bool
		}{{source, false}, {reference, true}} {
			if _, err := VerifyApprovedImageArray(ctx, db, objects, bucket, tenant, intent, actor, session, prepared, targets, request, edit.TargetID, entry.reference, entry.doc, "pixels", -1); err != nil {
				return nil, err
			}
			image, err := review.ParseWorkingImage(entry.doc.Body)
			if err != nil {
				return nil, ErrInvalid
			}
			if image.Alpha != nil {
				if _, err := VerifyApprovedImageArray(ctx, db, objects, bucket, tenant, intent, actor, session, prepared, targets, request, edit.TargetID, entry.reference, entry.doc, "alpha", -1); err != nil {
					return nil, err
				}
			}
			var chain struct {
				Steps []struct {
					Kind string `json:"kind"`
				}
			}
			if json.Unmarshal(image.TransformChain, &chain) != nil {
				return nil, ErrInvalid
			}
			for step, value := range chain.Steps {
				if value.Kind == "dense_warp" {
					if _, err := VerifyApprovedImageArray(ctx, db, objects, bucket, tenant, intent, actor, session, prepared, targets, request, edit.TargetID, entry.reference, entry.doc, "inverse_field", step); err != nil {
						return nil, err
					}
				}
			}
		}
		budget := costs[edit.TargetID]
		documents = append(documents, ExposureTargetDocuments{TargetID: edit.TargetID, Protection: pins[edit.TargetID], Documents: ExposureSourceDocuments{Source: source.Body, Reference: reference.Body, Mask: mask.Body, Protection: protection.Body}, OwnerCosts: budget.OwnerCosts, Limits: budget.Limits, Qualification: budget.Qualification})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ResolveApprovedExposureDocumentInputs(child, prepared, targets, request, documents)
}
