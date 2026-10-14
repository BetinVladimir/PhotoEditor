package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Bounded immutable dependency packet for composition with final skin admission.
// Serialization does not grant SQL authority or activate a task.
func (s SQLTransferSourceReader) marshalNativeTargetSkinDependencyEvidence(ctx context.Context, who auth.Identity, plan nativeTargetSkinDraftPlan) (json.RawMessage, error) {
	if err := verifyNativeTargetSkinPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	owned, err := s.compileNativeTargetSkinDraftPlan(ctx, who, plan.parser, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if owned.seal != plan.seal {
		return nil, v2input.ErrConflict
	}
	c := owned.prepared.params.parser
	digest, err := targetFaceResultDigest(c)
	if err != nil || digest != c.seal || c.planSeal != owned.parser.seal {
		return nil, v2input.ErrConflict
	}
	detector, err := s.marshalNativeTargetDetectorResultPins(ctx, who, owned.parser.detector, c.detector)
	if err != nil {
		return nil, err
	}
	for i, body := range []string{c.body, c.paramsBody} {
		entry := c.objects[i]
		digest := sha256.Sum256([]byte(body))
		if len(body) < 2 || len(body) > 1<<20 || entry.object.Bytes != int64(len(body)) || entry.object.ContentType != "application/json" || hex.EncodeToString(digest[:]) != entry.ref.SHA256 {
			return nil, v2input.ErrConflict
		}
	}
	files := make([]nativeDetectorResultLockPin, 3)
	for i, entry := range []defectEvidenceObject{c.pixels, c.objects[0], c.objects[1]} {
		role := []string{"face_labels", "face_labels_ref", "parser_params"}[i]
		files[i] = nativeDetectorResultLockPin{role, entry.ref.ArtifactID, entry.ref.SHA256, s.Bucket, entry.object.Key, entry.object.VersionID, entry.object.Bytes, entry.object.ContentType}
	}
	packet, err := canonical.Marshal(map[string]any{"schema_version": 1, "result_id": c.catalog.ResultID, "outputs": c.catalog.Outputs, "params": c.catalog.Params, "files": files, "detector": json.RawMessage(detector)})
	if err != nil || len(packet) > 32768 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return packet, nil
}
