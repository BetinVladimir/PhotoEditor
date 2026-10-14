package v2input

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// DecodePinnedSessionInputSnapshot verifies exact document bytes, complete
// required fields, contiguous targets and the normative digest excluding itself.
func DecodePinnedSessionInputSnapshot(ref Ref, raw json.RawMessage) (SessionInputSnapshot, error) {
	var empty SessionInputSnapshot
	if !validRef(ref) || len(raw) < 2 || len(raw) > 8<<20 {
		return empty, ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return empty, ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return empty, ErrInvalid
	}
	var snapshot SessionInputSnapshot
	if !semanticDecode(raw, &snapshot) || snapshot.SchemaVersion != 1 {
		return empty, ErrInvalid
	}
	encoded, err := canonical.Marshal(snapshot)
	if err != nil || !bytes.Equal(normalized, encoded) {
		return empty, ErrInvalid
	}
	expected, _, err := CompilePinnedSessionInputSnapshot(snapshot.SnapshotID, snapshot.Items)
	if err != nil || snapshot.SnapshotDigest != expected.SnapshotDigest {
		return empty, ErrInvalid
	}
	return expected, nil
}

// CompileSemanticSnapshotConfig freezes the snapshot root/digest and proves
// that every frozen transfer targets its exact ordinal/version/source/canvas.
// Live catalog/session/qualification authority remains an admission obligation.
func CompileSemanticSnapshotConfig(project, session, run uuid.UUID, snapshotRef Ref, raw json.RawMessage, tasks []FrozenSemanticTask) (SemanticDraftConfig, json.RawMessage, string, error) {
	var empty SemanticDraftConfig
	snapshot, err := DecodePinnedSessionInputSnapshot(snapshotRef, raw)
	if err != nil || len(tasks) != len(snapshot.Items) {
		return empty, nil, "", ErrInvalid
	}
	pins := make([]SemanticProtectionPin, len(tasks))
	for n, task := range tasks {
		pin, err := task.ProtectionPin()
		if err != nil || pin.Ordinal < 0 || pin.Ordinal >= len(snapshot.Items) || task.snapshotID != snapshot.SnapshotID {
			return empty, nil, "", ErrInvalid
		}
		params, err := DecodeSemanticTransferParams(task.closure.ParamsJCS)
		if err != nil || params.Target != snapshot.Items[pin.Ordinal].Binding {
			return empty, nil, "", ErrInvalid
		}
		pins[n] = pin
	}
	config, _, _, err := CompileSemanticDraftConfig(project, snapshot.SnapshotID, session, run, pins)
	if err != nil {
		return empty, nil, "", err
	}
	// Also prove that the supplied tasks belong to this run's deterministic graph.
	for _, task := range tasks {
		if task.task != config.Semantic.Nodes[task.protection.Ordinal].TaskID {
			return empty, nil, "", ErrInvalid
		}
	}
	value := snapshotRef
	config.Semantic.Snapshot = &value
	config.Semantic.SnapshotDigest = snapshot.SnapshotDigest
	body, err := canonical.Marshal(config)
	if err != nil || len(body) > 8<<20 {
		return empty, nil, "", ErrInvalid
	}
	digest, err := canonical.Hash(config)
	if err != nil {
		return empty, nil, "", err
	}
	return config, body, digest, nil
}
