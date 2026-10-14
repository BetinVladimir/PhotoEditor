package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Checks packet/result correspondence before SQL. Embedded admission authority
// is still independently validated by the existing original producer SQL locks.
func (s SQLTransferSourceReader) verifyNativeInputManualCompletedPackets(ctx context.Context, proof nativeInputManualDependencies) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	selection := proof.selection
	checks := []struct {
		body       string
		result     uuid.UUID
		outputs    []v2input.Ref
		params     v2input.Ref
		objects    []defectEvidenceObject
		roles      []string
		skinPixels *int64
	}{
		{proof.landmarks, selection.landmarks.catalog.ResultID, selection.landmarks.catalog.Outputs, selection.landmarks.catalog.Params, selection.landmarks.objects[:], []string{"face_landmarks", "params"}, nil},
		{proof.skin, selection.skin.catalog.ResultID, selection.skin.catalog.Outputs, selection.skin.catalog.Params, selection.skin.objects[:], []string{"skin_mask", "skin_mask_png", "skin_mask_ref", "params"}, &selection.skin.skinPixels},
	}
	for _, check := range checks {
		if len(check.body) < 2 || len(check.body) > 32<<20 || check.result == uuid.Nil {
			return v2input.ErrInvalid
		}
		var packet struct {
			Schema     int                       `json:"schema_version"`
			Admission  json.RawMessage           `json:"admission"`
			Result     uuid.UUID                 `json:"result_id"`
			Outputs    []v2input.Ref             `json:"outputs"`
			Params     v2input.Ref               `json:"params"`
			Files      []nativeSkinCompletedFile `json:"files"`
			SkinPixels *int64                    `json:"skin_pixels,omitempty"`
		}
		decoder := json.NewDecoder(bytes.NewBufferString(check.body))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&packet) != nil || decoder.Decode(new(any)) != io.EOF {
			return v2input.ErrInvalid
		}
		if packet.Schema != 1 || len(packet.Admission) < 2 || bytes.Equal(packet.Admission, []byte("null")) || packet.Result != check.result || packet.Params != check.params || !reflect.DeepEqual(packet.Outputs, check.outputs) || !reflect.DeepEqual(packet.SkinPixels, check.skinPixels) {
			return v2input.ErrConflict
		}
		files := make([]nativeSkinCompletedFile, len(check.objects))
		for i, object := range check.objects {
			files[i] = nativeSkinCompletedFile{Role: check.roles[i], Ref: object.ref, Bucket: s.Bucket, Key: object.object.Key, Version: object.object.VersionID, Bytes: object.object.Bytes, Media: object.object.ContentType}
		}
		if !reflect.DeepEqual(packet.Files, files) {
			return v2input.ErrConflict
		}
	}
	return ctx.Err()
}
