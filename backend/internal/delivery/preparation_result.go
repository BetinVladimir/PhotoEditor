package delivery

import (
	"bytes"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Preparation is the worker wire result, not permission to create child runs.
type Preparation struct {
	SchemaVersion  int         `json:"schema_version"`
	Input          v2input.Ref `json:"input"`
	SnapshotDigest string      `json:"snapshot_digest"`
	ItemCount      int         `json:"item_count"`
	SlotCount      int         `json:"slot_count"`
}

// ValidatePreparation requires the exact canonical output expected from this
// frozen plan and its physical artifact hash. Exact byte comparison also rejects
// duplicate keys, unknown fields, omitted fields, null/default coercion and
// noncanonical spellings without normalizing an untrusted worker document.
// The adoption transaction must separately check task fence/output provenance,
// current ACL, retention, quotas and atomically open the expansion barrier.
func (p PreparePlan) ValidatePreparation(ref v2input.Ref, raw []byte) (Preparation, error) {
	var zero Preparation
	expected := p.preparation
	if p.task == uuid.Nil || expected.SchemaVersion != 1 || expected.Input != p.input ||
		expected.ItemCount < 1 || expected.ItemCount > 5000 || expected.SlotCount < expected.ItemCount || expected.SlotCount > 80000 ||
		ref.ArtifactID == uuid.Nil || len(raw) < 2 || len(raw) > 8<<20 || digest(raw) != ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(expected)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	return expected, nil
}
