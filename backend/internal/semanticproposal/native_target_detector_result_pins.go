package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func (s SQLTransferSourceReader) marshalNativeTargetDetectorResultPins(ctx context.Context, who auth.Identity, plan nativeTargetDetectorPlan, proof nativeTargetDetectorResult) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return nil, err
	}
	if _, err := s.compileNativeTargetDetectorPlan(ctx, who, plan.anchor, plan.detector.prepared, plan.detector.policy, plan.detector.shell.Bytes); err != nil {
		return nil, err
	}
	digest, err := targetDetectorResultDigest(proof)
	if err != nil || digest != proof.seal || proof.planSeal != plan.seal {
		return nil, v2input.ErrConflict
	}
	completed := proof.completed
	task, err := plan.detector.prepared.frozen.DraftTask()
	if err != nil {
		return nil, err
	}
	if completed.catalog.ResultID == uuid.Nil || completed.catalog.Params != (v2input.Ref{ArtifactID: task.ParamsArtifactID, SHA256: task.ParamsSHA}) || completed.objects[0].ref != completed.catalog.Output || completed.objects[1].ref != completed.catalog.Params || !validFacePreparationObjects(completed.objects, [2]string{s.Bucket, s.Bucket}, s.Bucket, plan.anchor.entry.Binding.Source) {
		return nil, v2input.ErrInvalid
	}
	bodies := []string{completed.body, completed.detectorParamsBody, completed.sourceBody}
	refs := []v2input.Ref{completed.catalog.Output, completed.catalog.Params, plan.anchor.entry.Binding.Source}
	for i, body := range bodies {
		if len(body) < 2 || len(body) > 1<<20 {
			return nil, v2input.ErrInvalid
		}
		hash := sha256.Sum256([]byte(body))
		if hex.EncodeToString(hash[:]) != refs[i].SHA256 {
			return nil, v2input.ErrInvalid
		}
	}
	evidence := nativeDetectorResultLockEvidence{SchemaVersion: 1, ResultID: completed.catalog.ResultID, Output: completed.catalog.Output, Params: completed.catalog.Params}
	// SQL orders the exact object pins by role: detector_params before face_boxes.
	for i, index := range []int{1, 0} {
		entry := completed.objects[index]
		role := "face_boxes"
		if index == 1 {
			role = "detector_params"
		}
		if entry.object.ContentType != "application/json" || entry.object.Bytes != int64(len(bodies[index])) {
			return nil, v2input.ErrInvalid
		}
		evidence.Files[i] = nativeDetectorResultLockPin{role, entry.ref.ArtifactID, entry.ref.SHA256, s.Bucket, entry.object.Key, entry.object.VersionID, entry.object.Bytes, entry.object.ContentType}
	}
	raw, err := canonical.Marshal(evidence)
	if err != nil || len(raw) > 16384 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
