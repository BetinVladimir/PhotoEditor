package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Builds compiler inputs only for new landmark/parser children. Reports are
// unique current server-policy candidates and physically verified, not producer
// authority. Existing saved children retain their original compiler inputs.
func (s SQLTransferSourceReader) selectInitialFacePipelineConfiguration(ctx context.Context, who auth.Identity, tenantID uuid.UUID, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher) (nativeFacePipelineStartConfiguration, error) {
	var zero nativeFacePipelineStartConfiguration
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := validateTransferCapacityConfiguration(capacity); err != nil {
		return zero, err
	}
	if _, err := initialNativeFaceBudget(capacity, reserved, v2qualification.Metrics{}); err != nil {
		return zero, err
	}
	if documents == nil {
		return zero, v2input.ErrInvalid
	}
	snapshot, err := s.readQualificationPolicySnapshot(ctx, who, tenantID)
	if err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(snapshot, []string{"face.landmarks", "face.parse"})
	if err != nil {
		return zero, err
	}
	var used int64
	if err := chargeMetadata(&used, capacity.MetadataBytes, len(snapshot.body)); err != nil {
		return zero, err
	}
	configuration := nativeFacePipelineStartConfiguration{TransportDocuments: documents, Skin: nativePreparationBudget{RAM: capacity.RAM, Scratch: capacity.Scratch, Output: capacity.Output, Reserved: reserved}}
	var reports [2]v2input.Ref
	var pins [2]personEvidenceObject
	for i, operation := range []string{"face.landmarks", "face.parse"} {
		expected := ownQualificationExpected(selected[operation])
		if expected.Backend != "cuda" && expected.Backend != "mps" {
			return zero, v2qualification.ErrUnqualified
		}
		report, err := s.discoverFaceQualificationReport(ctx, who, tenantID, expected)
		if err != nil {
			return zero, err
		}
		pin, err := s.faceQualificationObject(ctx, who, tenantID, expected, report)
		if err != nil {
			return zero, err
		}
		if pin.object.Bytes < 2 || pin.object.Bytes > 8<<20 {
			return zero, v2input.ErrInvalid
		}
		if err := chargeMetadata(&used, capacity.MetadataBytes, int(pin.object.Bytes)); err != nil {
			return zero, err
		}
		verified, err := s.VerifyFaceQualificationTyped(ctx, who, tenantID, report, expected)
		if err != nil {
			return zero, err
		}
		current, err := s.faceQualificationObject(ctx, who, tenantID, expected, report)
		if err != nil {
			return zero, err
		}
		if current != pin {
			return zero, v2input.ErrConflict
		}
		budget, err := initialNativeFaceResourceBudget(capacity, reserved, verified.Resources())
		if err != nil {
			return zero, err
		}
		stage := nativeFacePipelineStageConfiguration{Report: report, Budget: budget}
		if i == 0 {
			configuration.Landmarks = stage
		} else {
			configuration.Parser = stage
		}
		reports[i], pins[i] = report, pin
	}
	// Later report IO must not leave earlier candidate or object pins unchecked.
	for i, operation := range []string{"face.landmarks", "face.parse"} {
		expected := selected[operation]
		current, err := s.faceQualificationObject(ctx, who, tenantID, expected, reports[i])
		if err != nil {
			return zero, err
		}
		if current != pins[i] {
			return zero, v2input.ErrConflict
		}
		report, err := s.discoverFaceQualificationReport(ctx, who, tenantID, expected)
		if err != nil {
			return zero, err
		}
		if report != reports[i] {
			return zero, v2input.ErrConflict
		}
	}
	if err := s.recheckNativePlanPolicy(ctx, who, tenantID, snapshot); err != nil {
		return zero, err
	}
	if err := configuration.validate(); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return configuration, nil
}
func initialNativeFaceBudget(capacity TransferPreparation, reserved int64, metrics v2qualification.Metrics) (nativePreparationBudget, error) {
	var zero nativePreparationBudget
	if err := validateTransferCapacityConfiguration(capacity); err != nil {
		return zero, err
	}
	costs, err := transferCostsFromVerifiedMetrics(metrics)
	if err != nil {
		return zero, err
	}
	if costs.RAMBytes > capacity.RAM || costs.VRAMBytes > capacity.VRAM || costs.ScratchBytes > capacity.Scratch {
		return zero, v2input.ErrInvalid
	}
	budget := nativePreparationBudget{Costs: costs, RAM: capacity.RAM, VRAM: capacity.VRAM, Scratch: capacity.Scratch, Output: capacity.Output, Reserved: reserved}
	if err := validateNativeFaceStartBudget(budget); err != nil {
		return zero, err
	}
	return budget, nil
}

// Automatic server-policy assembly entry point. It selects no report from user
// intent; durable scheduling, the original parent lock and public parent receipt
// remain owned by the caller.
func (b nativePreparationBootstrap) advanceQualifiedInitialFacePipeline(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher) (nativeFacePipelineAdvance, error) {
	if err := b.validate(ctx); err != nil {
		return nativeFacePipelineAdvance{}, err
	}
	configuration, err := b.Reader.selectInitialFacePipelineConfiguration(ctx, who, anchor.Request.TenantID, ownTransferPreparation(capacity), reserved, documents)
	if err != nil {
		return nativeFacePipelineAdvance{}, err
	}
	return b.advanceInitialFacePipeline(ctx, who, anchor, configuration)
}
