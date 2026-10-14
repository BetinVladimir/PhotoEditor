package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type Manifest struct {
	SchemaVersion  int           `json:"schema_version"`
	AssemblySHA    string        `json:"assembly_sha"`
	SnapshotDigest string        `json:"snapshot_digest"`
	SlotCount      int           `json:"slot_count"`
	ReadyCount     int           `json:"ready_count"`
	OmittedCount   int           `json:"omitted_count"`
	Shards         []v2input.Ref `json:"shards"`
}

// ValidateManifest verifies immutable bytes and reconstructs the expected result
// from the exact frozen assembly matrix. It grants no DB adoption authority;
// physical version/producer/current source checks remain repository obligations.
func (p PreparePlan) ValidateManifest(ctx context.Context, parent uuid.UUID, root v2input.Ref, rootRaw []byte, shards [][]byte, result v2input.Ref, resultRaw []byte) (Manifest, error) {
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	if result.ArtifactID == uuid.Nil || !shaPattern.MatchString(result.SHA256) || len(resultRaw) < 2 || len(resultRaw) > 8<<20 || digest(resultRaw) != result.SHA256 {
		return Manifest{}, v2input.ErrInvalid
	}
	if _, err := p.AssemblyRequiredInputs(ctx, parent, root, rootRaw, shards); err != nil {
		return Manifest{}, err
	}
	var assembly AssemblyInput
	if json.Unmarshal(rootRaw, &assembly) != nil {
		return Manifest{}, v2input.ErrInvalid
	}
	expected := Manifest{SchemaVersion: 1, AssemblySHA: root.SHA256, SnapshotDigest: assembly.SnapshotDigest, SlotCount: assembly.SlotCount, Shards: append([]v2input.Ref(nil), assembly.Shards...)}
	for _, raw := range shards {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		var shard SlotShard
		if json.Unmarshal(raw, &shard) != nil {
			return Manifest{}, v2input.ErrInvalid
		}
		for _, slot := range shard.Items {
			switch slot.State {
			case "ready":
				expected.ReadyCount++
			case "omitted":
				expected.OmittedCount++
			default:
				return Manifest{}, v2input.ErrInvalid
			}
		}
	}
	if expected.ReadyCount < 1 || expected.ReadyCount+expected.OmittedCount != expected.SlotCount {
		return Manifest{}, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(expected)
	if err != nil {
		return Manifest{}, err
	}
	if !bytes.Equal(exact, resultRaw) {
		return Manifest{}, v2input.ErrInvalid
	}
	return expected, nil
}
