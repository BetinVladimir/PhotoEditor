package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// AssemblyRequiredInputs derives roots from exact published bytes and the frozen
// preparation intent, not a caller-supplied dependency list. EncodedExport denotes
// the binary raster referenced by a slot, not its EncodedImage JSON manifest.
// SQL/physical authority and transitive resolution are still admission duties.
func (p PreparePlan) AssemblyRequiredInputs(ctx context.Context, parent uuid.UUID, root v2input.Ref, rootRaw []byte, shards [][]byte) ([]v2input.Entry, error) {
	invalid := func() ([]v2input.Entry, error) { return nil, v2input.ErrInvalid }
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if root.ArtifactID == uuid.Nil || len(rootRaw) < 2 || len(rootRaw) > 1<<20 || digest(rootRaw) != root.SHA256 || len(shards) < 1 || len(shards) > 160 {
		return invalid()
	}
	var input AssemblyInput
	var proof PrepareProof
	var frozen FrozenInput
	if json.Unmarshal(rootRaw, &input) != nil || json.Unmarshal(p.proofJCS, &proof) != nil || json.Unmarshal([]byte(proof.FrozenInputJCS), &frozen) != nil || len(input.Shards) != len(shards) || len(shards) != (p.preparation.SlotCount+499)/500 {
		return invalid()
	}
	slots := make([]Slot, 0, p.preparation.SlotCount)
	for index, raw := range shards {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(raw) < 2 || len(raw) > 1<<20 || digest(raw) != input.Shards[index].SHA256 {
			return invalid()
		}
		var shard SlotShard
		if json.Unmarshal(raw, &shard) != nil || shard.SchemaVersion != 1 || shard.SnapshotDigest != frozen.SnapshotDigest || len(shard.Items) < 1 || len(shard.Items) > 500 {
			return invalid()
		}
		slots = append(slots, shard.Items...)
	}
	if len(slots) != p.preparation.SlotCount {
		return invalid()
	}
	entries := []v2input.Entry{{Ref: root, Contract: "DeliveryAssemblyInput"}}
 entries=append(entries,p.watermarkAssemblyRoots...)
	for _, preset := range frozen.Presets {
		entries = append(entries, v2input.Entry{Ref: preset.Config, Contract: "ExportPresetConfig"})
	}
	for cursor := 0; cursor < len(slots); {
		page, err := p.ExportChildren(parent, cursor, 100)
		if err != nil {
			return nil, err
		}
		for _, child := range page.Items {
			slot := slots[child.SlotIndex]
			if slot.Ordinal != child.Ordinal || slot.Binding != child.Binding || slot.PresetID != child.Preset.PresetID {
				return invalid()
			}
			if slot.State == "ready" {
				exportID, err := p.NewChildExportID(parent, child)
				if err != nil {
					return nil, err
				}
				if slot.ExportID == nil || *slot.ExportID != exportID || slot.Export == nil {
					return invalid()
				}
				entries = append(entries, v2input.Entry{Ref: *slot.Export, Contract: "EncodedExport"})
			}
			entries = append(entries, v2input.Entry{Ref: child.Binding.Source, Contract: "WorkingImage"}, v2input.Entry{Ref: child.Recipe, Contract: "FrozenRenderRecipe"})
		}
		cursor = page.NextCursor
	}
	index := 0
	_, exact, _, err := BuildAssemblyInput(ctx, frozen, []byte(proof.SnapshotJCS), slots, func(_ context.Context, purpose string, raw []byte) (v2input.Ref, error) {
		if index >= len(shards) || purpose != fmt.Sprintf("v30_delivery_slots_%03d", index) || !bytes.Equal(raw, shards[index]) {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		ref := input.Shards[index]
		index++
		return ref, nil
	})
	if err != nil {
		return nil, err
	}
	if index != len(shards) || !bytes.Equal(exact, rootRaw) {
		return invalid()
	}
	for _, ref := range input.Shards {
		entries = append(entries, v2input.Entry{Ref: ref, Contract: "DeliverySlotShard"})
	}
	seen := map[uuid.UUID]v2input.Entry{}
	for _, entry := range entries {
		if entry.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(entry.Ref.SHA256) {
			return invalid()
		}
		if previous, ok := seen[entry.Ref.ArtifactID]; ok && previous != entry {
			return invalid()
		}
		seen[entry.Ref.ArtifactID] = entry
	}
	result := make([]v2input.Entry, 0, len(seen))
	for _, entry := range seen {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Ref.ArtifactID.String() < result[j].Ref.ArtifactID.String() })
	return result, nil
}
