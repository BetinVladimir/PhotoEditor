package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func digestDustBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// RemapDustProposalFrames remaps the entire ordered series. Candidate identity
// depends on every frame SHA, so a single-frame substitution is never valid.
// The caller must independently prove each new frame's source/tenant binding.
func RemapDustProposalFrames(ctx context.Context, raw []byte, original, target ArtifactRef, remap func(context.Context, ArtifactRef) (ArtifactRef, error)) ([]byte, map[string]string, error) {
	fail := func() ([]byte, map[string]string, error) { return nil, nil, errors.New("invalid sensor dust remap") }
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if remap == nil || !original.valid() || !target.valid() || original == target {
		return fail()
	}
	parsed, err := ReadDustProposal(ctx, bytes.NewReader(raw), ArtifactRef{ID: uuid.New(), SHA256: digestDustBytes(raw)}, original)
	if err != nil {
		return nil, nil, err
	}
	doc := parsed.document
	hashes := make([]string, len(doc.Frames))
	seen := map[uuid.UUID]bool{}
	hasTarget := false
	for i, frame := range doc.Frames {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		mapped, err := remap(ctx, frame)
		if err != nil {
			return nil, nil, err
		}
		if !mapped.valid() || seen[mapped.ID] || mapped == frame || (frame == original && mapped != target) {
			return fail()
		}
		seen[mapped.ID] = true
		hasTarget = hasTarget || mapped == target
		doc.Frames[i] = mapped
		hashes[i] = mapped.SHA256
	}
	if !hasTarget {
		return fail()
	}
	ids := make(map[string]string, len(doc.Candidates))
	for i := range doc.Candidates {
		candidate := &doc.Candidates[i]
		id, err := canonical.Hash(map[string]any{"algorithm": doc.Algorithm, "camera_id": doc.CameraID, "sensor": []int64{doc.SensorWidth, doc.SensorHeight}, "x": candidate.SensorX, "y": candidate.SensorY, "radius": candidate.Radius, "frames": hashes})
		if err != nil {
			return nil, nil, err
		}
		ids[candidate.ID] = id
		candidate.ID = id
	}
	result, err := canonical.Marshal(doc)
	if err != nil {
		return nil, nil, err
	}
	if _, err := ReadDustProposal(ctx, bytes.NewReader(result), ArtifactRef{ID: uuid.New(), SHA256: digestDustBytes(result)}, target); err != nil {
		return nil, nil, err
	}
	return result, ids, nil
}
