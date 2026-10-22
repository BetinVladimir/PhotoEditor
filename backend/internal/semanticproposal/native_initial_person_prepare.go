package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Caller holds the original parent lock. The initial scheduling checkpoint stays
// active across preparation/publication failures. Existing person checkpoints are
// never replaced or requalified against new compiler inputs. A child receipt
// proves admission only; it is neither person output readiness nor parent acceptance.
func (b nativePreparationBootstrap) ensureInitialPersonPreparation(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher) error {
	if err := b.validate(ctx); err != nil {
		return err
	}
	if documents == nil {
		return v2input.ErrInvalid
	}
	detector, err := b.Reader.restoreDetectorDraftPlan(ctx, b.Catalog, who, anchor.Request, anchor.Key, anchor.Published, anchor.Snapshot, anchor.SnapshotBody, anchor.Color)
	if err != nil {
		return err
	}
	presence, err := b.Reader.readNativeStagePresence(ctx, who, detector, "mask.person")
	if err != nil {
		return err
	}
	switch presence {
	case nativeStagePresent:
		person, err := b.Reader.restorePersonDraftPlan(ctx, b.Catalog, who, anchor)
		if err != nil {
			return err
		}
		state, err := b.Reader.readNativeInputSavedStageState(ctx, who, anchor.Request, anchor.Published.Ref, anchor.Snapshot, "mask.person")
		if err != nil {
			return err
		}
		if !state.sealed {
			run, _, err := personPreparationIDs(anchor.Request.RunID)
			if err != nil {
				return err
			}
			publisher := v2input.ScopedPublisher{Documents: documents, Tenant: anchor.Request.TenantID, Run: run}
			if _, err := b.Reader.persistPersonPreparationInputs(ctx, b.Scheduler, publisher, who, person, anchor.Color); err != nil {
				return err
			}
		}
		_, err = b.Reader.resumePersonPreparation(ctx, b.Scheduler, who, person)
		return err
	case nativeStageConflict:
		return v2input.ErrConflict
	case nativeStageAbsent:
	default:
		return v2input.ErrConflict
	}
	policy, err := b.Reader.selectNativePersonProtectionPolicy(ctx, who, anchor.Request.TenantID, capacity.MetadataBytes)
	if err != nil {
		return err
	}
	if policy.Expected.Backend != "cuda" && policy.Expected.Backend != "mps" {
		return v2qualification.ErrUnqualified
	}
	pin, err := b.Reader.personQualificationObject(ctx, who, anchor.Request.TenantID, policy.Expected, policy.Report)
	if err != nil {
		return err
	}
	if pin.object.Bytes < 2 || pin.object.Bytes > capacity.MetadataBytes || pin.object.Bytes > 8<<20 {
		return v2input.ErrInvalid
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, b.Reader.Objects, pin.object, policy.Report)
	if err != nil {
		return err
	}
	verified, err := v2qualification.VerifyTyped(raw, policy.Report.SHA256, policy.Expected)
	if err != nil {
		return err
	}
	budget, err := initialNativeFaceResourceBudget(capacity, reserved, verified.Resources())
	if err != nil {
		return err
	}
	if budget.Costs.VRAMBytes <= 0 {
		return v2qualification.ErrUnqualified
	}
	current, err := b.Reader.personQualificationObject(ctx, who, anchor.Request.TenantID, policy.Expected, policy.Report)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(pin, current) {
		return v2input.ErrConflict
	}
	run, _, err := personPreparationIDs(anchor.Request.RunID)
	if err != nil {
		return err
	}
	personBootstrap := b
	personBootstrap.Transport = v2input.ScopedPublisher{Documents: documents, Tenant: anchor.Request.TenantID, Run: run}
	// Guided tile is a server-owned bound, not an input inferred from the user's mask.
	_, _, err = personBootstrap.bootstrapPerson(ctx, who, anchor, policy.Report, 512, budget)
	return err
}
