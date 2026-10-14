package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Builds a manual downstream packet with the complete nested manual admission.
// Only the separate manual SQL gate may accept it; never unwrap to bare evidence.
// Rereads physical native defect and producer content
// and exact current catalogs. A future SQL dependency lock must repeat authority
// in the parent admission transaction; this packet alone grants no capability.
func (s SQLTransferSourceReader) marshalNativeInputManualInitialCompletedEvidence(ctx context.Context, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile, completed completedNativeInputDefect, built v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return nil, err
	}
	current, err := s.readCompletedNativeInputManualInitialDefect(ctx, who, plan, color)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(current, completed) || current.catalog.ResultID == uuid.Nil {
		return nil, v2input.ErrConflict
	}
	admission, err := s.marshalNativeInputManualInitialAdmissionEvidence(ctx, who, plan, built)
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
