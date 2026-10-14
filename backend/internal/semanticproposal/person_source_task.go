package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type sourcePersonPreparation struct {
 legacyQueueOmitted bool
	continuation   string
	ordinaryPolicy *createProjectPolicy
	costs          v2input.SemanticQualifiedCosts
	qualification  personQualificationProof
	parent         uuid.UUID
	document       uuid.UUID
	catalog        catalogPersonPreparation
	source         sourcePreparationProof
}

func personPreparationIDs(parent uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if parent == uuid.Nil {
		return uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	run := uuid.NewSHA1(parent, []byte("semantic.mask.person:reference"))
	workflow, err := v2input.WorkflowID(run)
	return run, workflow, err
}

// Verifies exact original RGB/alpha/geometry bytes and deployed ICC before
// deriving independent qualified LRASPP inputs. Trusted report verification and
// final SQL locks remain required; this preparation grants no queue authority.
func (s SQLTransferSourceReader) prepareSourcePersonTask(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenant, parent uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, document uuid.UUID, params, report v2input.Ref, expected v2qualification.Expected, tile int, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64, profileVersions ...int) (sourcePersonPreparation, error) {
	var zero sourcePersonPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || document == uuid.Nil || tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	expected = ownQualificationExpected(expected)
	if expected.Operation != "mask.person" || expected.Version != "1" || expected.Backend != "cuda" {
		return zero, v2qualification.ErrUnqualified
	}
	_, workflow, err := personPreparationIDs(parent)
	if err != nil {
		return zero, err
	}
	_, proof, err := s.readQualifiedSourcePreparationProof(ctx, who, tenant, binding, color, true)
	if err != nil {
		return zero, err
	}
	body, err := s.Read(ctx, who, tenant, binding)
	if err != nil {
		return zero, err
	}
	scoped := detectorParamsCatalog{reader: s, source: catalog, binding: binding, document: document, params: params}
	qualified, err := s.prepareQualifiedCatalogPersonTask(ctx, scoped, who, tenant, workflow, 0, binding, body, params, report, expected, tile, costs, ram, vram, scratch, output, profileVersions...)
	if err != nil {
		return zero, err
	}
	if err := s.recheckSourcePreparationProof(ctx, who, tenant, binding, color, true, proof); err != nil {
		return zero, err
	}
	prepared := catalogPersonPreparation{frozen: qualified.frozen, inputs: qualified.inputs}
	if err := verifyPersonSourceInputPins(ctx, prepared, proof); err != nil {
		return zero, err
	}
	if err := verifyPersonTaskQualificationBinding(prepared.frozen, qualified.qualification); err != nil {
		return zero, err
	}
	if err := s.recheckPersonQualificationProof(ctx, who, tenant, qualified.qualification, expected); err != nil {
		return zero, err
	}
	return sourcePersonPreparation{costs: costs, parent: parent, document: document, catalog: prepared, source: proof, qualification: qualified.qualification}, nil
}

func verifyPersonStageIdentity(parent uuid.UUID, prepared sourcePersonPreparation) error {
	_, workflow, err := personPreparationIDs(parent)
	if err != nil {
		return err
	}
	expected, err := v2input.TaskID(workflow, "mask.person", 0)
	if err != nil {
		return err
	}
	draft, err := prepared.catalog.frozen.DraftTask()
	if err != nil {
		return err
	}
	if prepared.parent != parent || draft.ID != expected {
		return v2input.ErrConflict
	}
	return nil
}
