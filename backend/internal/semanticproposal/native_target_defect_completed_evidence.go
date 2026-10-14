package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Builds downstream evidence only after rereading physical native defect and producer content
// and exact current catalogs. A future SQL dependency lock must repeat authority
// in the parent admission transaction; this packet alone grants no capability.
func (s SQLTransferSourceReader) marshalNativeTargetDefectCompletedEvidence(ctx context.Context, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan, dependencies nativeTargetDefectDependencies, color TransferColorProfile, completed completedNativeTargetDefect, built v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	current, err := s.readCompletedNativeTargetDefect(ctx, who, plan, color)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(current, completed) || current.catalog.ResultID == uuid.Nil {
		return nil, v2input.ErrConflict
	}
	admission, err := s.marshalNativeTargetDefectAdmissionEvidence(ctx, who, original, plan, dependencies, built)
	if err != nil {
		return nil, err
	}
	roles := []string{"defect_labels", "defect_protection", "defect_labels_png", "defect_candidates", "defect_propose_report", "params"}
	files := make([]nativeSkinCompletedFile, 6)
	for i, entry := range current.objects {
		files[i] = nativeSkinCompletedFile{Role: roles[i], Ref: entry.ref, Bucket: s.Bucket, Key: entry.object.Key, Version: entry.object.VersionID, Bytes: entry.object.Bytes, Media: entry.object.ContentType}
	}
	packet, err := canonical.Marshal(nativeDefectCompletedEvidence{SchemaVersion: 1, Admission: admission, ResultID: current.catalog.ResultID, Outputs: current.catalog.Outputs, Params: current.catalog.Params, Files: files})
	if err != nil {
		return nil, err
	}
	if len(packet) > 32<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return packet, nil
}
