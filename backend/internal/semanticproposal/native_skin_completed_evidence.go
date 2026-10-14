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

type nativeSkinCompletedEvidence struct {
	SchemaVersion int                       `json:"schema_version"`
	Admission     json.RawMessage           `json:"admission"`
	ResultID      uuid.UUID                 `json:"result_id"`
	Outputs       []v2input.Ref             `json:"outputs"`
	Params        v2input.Ref               `json:"params"`
	Files         []nativeSkinCompletedFile `json:"files"`
	SkinPixels    int64                     `json:"skin_pixels"`
}

type nativeSkinCompletedFile struct {
	Role    string      `json:"role"`
	Ref     v2input.Ref `json:"ref"`
	Bucket  string      `json:"bucket"`
	Key     string      `json:"key"`
	Version string      `json:"version"`
	Bytes   int64       `json:"bytes"`
	Media   string      `json:"media"`
}

// Builds downstream evidence only after rereading physical skin/parser content
// and exact current catalogs. A future SQL dependency lock must repeat authority
// in the parent admission transaction; this packet alone grants no capability.
func (s SQLTransferSourceReader) marshalNativeSkinCompletedEvidence(ctx context.Context, who auth.Identity, plan nativeSkinDraftPlan, color TransferColorProfile, completed completedNativeSkin, built, parserBuilt, detectorBuilt v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeSkinPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	current, err := s.readCompletedNativeSkin(ctx, who, plan, color)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(current, completed) || current.catalog.ResultID == uuid.Nil || current.skinPixels < 0 {
		return nil, v2input.ErrConflict
	}
	admission, err := s.marshalNativeSkinAdmissionEvidence(ctx, who, plan, built, parserBuilt, detectorBuilt)
	if err != nil {
		return nil, err
	}
	roles := []string{"skin_mask", "skin_mask_png", "skin_mask_ref", "params"}
	files := make([]nativeSkinCompletedFile, 4)
	for i, entry := range current.objects {
		files[i] = nativeSkinCompletedFile{Role: roles[i], Ref: entry.ref, Bucket: s.Bucket, Key: entry.object.Key, Version: entry.object.VersionID, Bytes: entry.object.Bytes, Media: entry.object.ContentType}
	}
	packet, err := canonical.Marshal(nativeSkinCompletedEvidence{SchemaVersion: 1, Admission: admission, ResultID: current.catalog.ResultID, Outputs: current.catalog.Outputs, Params: current.catalog.Params, Files: files, SkinPixels: current.skinPixels})
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
