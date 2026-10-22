package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// BuildRetouchPreviewContext freezes server-resolved faces and explicit beauty
// controls. It does not prove landmark/skin origin or grant dispatch authority;
// existing SQL provenance and qualification gates must independently do so.
func BuildRetouchPreviewContext(params RetouchPreviewParams, transferred v2input.Ref,
	mask []byte, faces []RetouchPreviewFace, beauty RetouchPreviewBeauty) (v2input.Ref, []byte, error) {
	invalid := func() (v2input.Ref, []byte, error) { return v2input.Ref{}, nil, ErrInvalid }
	_, paramsSHA, err := FreezeRetouchPreviewParams(params)
	if err != nil || !validRetouchBeauty(beauty) || len(faces) < 1 || len(faces) > 64 || len(mask) < 2 || len(mask) > 1048576 {
		return invalid()
	}
	owned := make([]RetouchPreviewFace, len(faces))
	for i, face := range faces {
		// Null is an explicit conservative-mask choice; an omitted resolver
		// result must not silently acquire that meaning during JSON marshaling.
		if len(face.Skin) == 0 {
			return invalid()
		}
		owned[i] = face
		if face.ObjectID != nil {
			id := *face.ObjectID
			owned[i].ObjectID = &id
		}
		owned[i].Skin = append(json.RawMessage(nil), face.Skin...)
		// Manual application is exactly the transferred candidate mask, never a
		// caller-selected second region or an unbounded full-face fallback.
		owned[i].Manual = append(json.RawMessage(nil), mask...)
		owned[i].ManualMode = true
	}
	context := RetouchPreviewAdapterContext{SchemaVersion: 1, Algorithm: "beautyfilter_v1", Source: params.Source,
		Candidate: params.Candidate, TransferredMask: transferred, Protection: params.Protection, Faces: owned, Beauty: beauty,
		ModelLockSHA: params.ModelLockSHA, RuntimeLockSHA: params.RuntimeLockSHA, DeviceProfileSHA: params.DeviceProfileSHA}
	raw, err := canonical.Marshal(context)
	if err != nil || len(raw) > 1048576 {
		return invalid()
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(params.TenantID, []byte("semantic.retouch.adapter_context:v1:"+paramsSHA+":"+digest)), SHA256: digest}
	if _, _, err := BindRetouchPreviewContext(params, ref, raw, mask); err != nil {
		return invalid()
	}
	return ref, append([]byte(nil), raw...), nil
}
