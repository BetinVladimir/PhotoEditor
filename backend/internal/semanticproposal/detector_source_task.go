package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Retains original source pins for final transaction locking. This value is
// preparatory only; model/policy and every task-input pin still need SQL locks.
type sourceDetectorPreparation struct {
	legacyTargetQueueOmitted bool
	costs                    v2input.SemanticQualifiedCosts
	parent                   uuid.UUID
	document                 uuid.UUID
	inputs                   string
	qualification            detectorQualificationProof
	frozen                   v2input.FrozenSemanticDetectorTask
	source                   sourcePreparationProof
}

// prepareSourceDetectorTask verifies actual original RGB/alpha/geometry and ICC
// using the existing unit-interval face adapter, then composes qualified SFD
// preparation. It neither clips HDR source nor alters image processing quality.
func (s SQLTransferSourceReader) prepareSourceDetectorTask(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenantID, parent uuid.UUID, ordinal int, binding v2input.SessionVersionBinding, color TransferColorProfile, document uuid.UUID, params, report v2input.Ref, expected v2qualification.Expected, score float64, maxFaces int, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64, profileVersions ...int) (sourceDetectorPreparation, error) {
	var zero sourceDetectorPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if document == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	_, workflow, err := detectorPreparationIDs(parent)
	if err != nil || ordinal != 0 {
		return zero, v2input.ErrInvalid
	}
	expected = ownQualificationExpected(expected)
	if expected.Operation != "face.detect" || expected.Version != "1" || (expected.Backend != "cuda" && expected.Backend != "mps") || catalog == nil {
		return zero, v2qualification.ErrUnqualified
	}
	_, source, err := s.readQualifiedSourcePreparationProof(ctx, who, tenantID, binding, color, true)
	if err != nil {
		return zero, err
	}
	body, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return zero, err
	}
	paramsCatalog := detectorParamsCatalog{reader: s, source: catalog, binding: binding, document: document, params: params}
	frozen, err := s.prepareQualifiedCatalogDetectorTask(ctx, paramsCatalog, who, tenantID, workflow, ordinal, binding, body, params, report, expected, score, maxFaces, costs, ram, vram, scratch, output, profileVersions...)
	if err != nil {
		return zero, err
	}
	if err := s.recheckSourcePreparationProof(ctx, who, tenantID, binding, color, true, source); err != nil {
		return zero, err
	}
	prepared := sourceDetectorPreparation{costs: costs, parent: parent, document: document, inputs: frozen.inputs, frozen: frozen.frozen, source: source, qualification: frozen.qualification}
	if err := verifyDetectorSourceInputPins(ctx, prepared); err != nil {
		return zero, err
	}
	return prepared, nil
}

// lockSourceDetectorPreparation composes original source and deployment report
// locks in the caller's admission transaction. Caller rolls back every error;
// task-input registration, snapshot/policy barriers and quota remain separate.
func (s SQLTransferSourceReader) lockSourceDetectorPreparation(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, prepared sourceDetectorPreparation, expected v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !prepared.source.unitRGB || prepared.source.iccSHA != color.sha {
		return v2input.ErrInvalid
	}
	if err := verifyDetectorStageIdentity(prepared.parent, prepared); err != nil {
		return err
	}
	if err := verifyDetectorTaskQualificationBinding(prepared.frozen, prepared.qualification); err != nil {
		return err
	}
	if err := verifyDetectorSourceInputPins(ctx, prepared); err != nil {
		return err
	}
	if _, err := s.marshalDetectorQualificationProof(ctx, who, tenant, prepared.qualification, expected); err != nil {
		return err
	}
	if _, err := marshalDetectorParamsPin(ctx, s.Bucket, prepared); err != nil {
		return err
	}
	if err := s.lockSourcePreparationProof(ctx, tx, who, tenant, binding, prepared.source); err != nil {
		return err
	}
	if err := s.lockDetectorQualificationProof(ctx, tx, who, tenant, prepared.qualification, expected); err != nil {
		return err
	}
	return s.lockDetectorParams(ctx, tx, who, tenant, binding, prepared)
}
