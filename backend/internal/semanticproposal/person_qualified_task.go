package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type personQualificationProof struct {
	taskSHA                         string
	tenant, project, actor, session uuid.UUID
	bucket, expected                string
	report                          personEvidenceObject
	metrics                         v2qualification.Metrics
}
type qualifiedPersonPreparation struct {
	inputs        string
	frozen        v2input.FrozenSemanticPersonTask
	qualification personQualificationProof
}

// prepareQualifiedCatalogPersonTask binds the original trusted report and its
// measured costs to new LRASPP preparation. It rechecks live report authority after
// catalog IO. Source pixel/ICC verification and transactional admission locks
// still must accompany this value; this function never activates a task.
func (s SQLTransferSourceReader) prepareQualifiedCatalogPersonTask(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenantID, workflow uuid.UUID, ordinal int, binding v2input.SessionVersionBinding, imageBody json.RawMessage, params, report v2input.Ref, expected v2qualification.Expected, guidedTile int, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64, profileVersions ...int) (qualifiedPersonPreparation, error) {
	var zero qualifiedPersonPreparation
	version := 1
	if len(profileVersions) > 1 {
		return zero, v2input.ErrInvalid
	}
	if len(profileVersions) == 1 {
		version = profileVersions[0]
	}
	if version != 0 && version != 1 {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	expected = ownQualificationExpected(expected)
	if expected.Operation != "mask.person" || expected.Version != "1" || expected.Backend != "cuda" {
		return zero, v2qualification.ErrUnqualified
	}
	if err := validateTransferMeasuredCosts(costs, v2qualification.Metrics{}); err != nil {
		return zero, err
	}
	expectedBody, err := canonical.Marshal(expected)
	if err != nil || len(expectedBody) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	imageBody = append([]byte(nil), imageBody...)
	// Reject Params/source contradictions before report/object-store IO.
	if _, err := v2input.CompileSemanticPersonClosure(ctx, workflow, ordinal, binding, imageBody, params, expected.ModelLockSHA, guidedTile); err != nil {
		return zero, err
	}
	original, err := s.personQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, original.object, original.ref)
	if err != nil {
		return zero, err
	}
	verified, err := v2qualification.Verify(raw, original.ref.SHA256, expected)
	if err != nil {
		return zero, err
	}
	if err := validateTransferMeasuredCosts(costs, verified.Metrics); err != nil {
		return zero, err
	}
	current, err := s.personQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return zero, err
	}
	if current != original {
		return zero, v2input.ErrConflict
	}
	frozen, err := prepareCatalogPersonTask(ctx, catalog, who, tenantID, workflow, ordinal, binding, imageBody, params, expected.ModelLockSHA, guidedTile, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	current, err = s.personQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return zero, err
	}
	if current != original {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if version == 1 {
		frozen.frozen, err = frozen.frozen.WithExecutionIdentity(expected.ModelLockSHA, expected.RuntimeLockSHA, expected.DeviceProfileSHA)
		if err != nil {
			return zero, err
		}
	}
	draft, err := frozen.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var admission struct {
		Resources struct {
			RAM     int64 `json:"ram_bytes"`
			VRAM    int64 `json:"vram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
			Output  int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(draft.ExecutionProfile, &admission) != nil || admission.Resources.RAM > ram || admission.Resources.VRAM > vram || admission.Resources.Scratch > scratch || admission.Resources.Output > output {
		return zero, v2input.ErrInvalid
	}
	taskSHA, err := canonical.Hash(draft)
	if err != nil {
		return zero, err
	}
	return qualifiedPersonPreparation{frozen: frozen.frozen, inputs: frozen.inputs, qualification: personQualificationProof{taskSHA: taskSHA, tenant: tenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, expected: string(expectedBody), report: original, metrics: verified.Metrics}}, nil
}
