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

type detectorQualificationProof struct {
	taskSHA                         string
	tenant, project, actor, session uuid.UUID
	bucket, expected                string
	report                          personEvidenceObject
	metrics                         v2qualification.Metrics
}
type qualifiedDetectorPreparation struct {
	inputs        string
	frozen        v2input.FrozenSemanticDetectorTask
	qualification detectorQualificationProof
}

// prepareQualifiedCatalogDetectorTask binds the original trusted report and its
// measured costs to new SFD preparation. It rechecks live report authority after
// catalog IO. Source pixel/ICC verification and transactional admission locks
// still must accompany this value; this function never activates a task.
func (s SQLTransferSourceReader) prepareQualifiedCatalogDetectorTask(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenantID, workflow uuid.UUID, ordinal int, binding v2input.SessionVersionBinding, imageBody json.RawMessage, params, report v2input.Ref, expected v2qualification.Expected, score float64, maxFaces int, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64, profileVersions ...int) (qualifiedDetectorPreparation, error) {
	var zero qualifiedDetectorPreparation
	version, err := nativeModelProfileVersion(profileVersions)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	expected = ownQualificationExpected(expected)
	if expected.Operation != "face.detect" || expected.Version != "1" || expected.Backend != "cuda" {
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
	if _, err := v2input.CompileSemanticDetectorClosure(ctx, workflow, ordinal, binding, imageBody, params, expected.ModelLockSHA, score, maxFaces); err != nil {
		return zero, err
	}
	original, err := s.faceQualificationObject(ctx, who, tenantID, expected, report)
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
	current, err := s.faceQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return zero, err
	}
	if current != original {
		return zero, v2input.ErrConflict
	}
	frozen, err := prepareCatalogDetectorTask(ctx, catalog, who, tenantID, workflow, ordinal, binding, imageBody, params, expected.ModelLockSHA, score, maxFaces, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	current, err = s.faceQualificationObject(ctx, who, tenantID, expected, report)
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
	if err := validateNativeModelProfileCapacity(draft, ram, vram, scratch, output); err != nil {
		return zero, err
	}
	taskSHA, err := canonical.Hash(draft)
	if err != nil {
		return zero, err
	}
	return qualifiedDetectorPreparation{frozen: frozen.frozen, inputs: frozen.inputs, qualification: detectorQualificationProof{taskSHA: taskSHA, tenant: tenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, expected: string(expectedBody), report: original, metrics: verified.Metrics}}, nil
}
