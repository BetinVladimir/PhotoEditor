package v2sessions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func exactCanonicalJSON(raw []byte, expectedSHA string, limit int) bool {
	if len(raw) < 2 || len(raw) > limit || !digestPattern.MatchString(expectedSHA) {
		return false
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalBody, raw) {
		return false
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]) == expectedSHA
}

func exactKeys(raw []byte, names ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	return true
}

// validateCorrectionPlan checks the contract and its relationship to the
// exact frozen source versions in one ScenePlan group. Group ordinals are
// global snapshot ordinals, not positions within the CorrectionPlan array.
func validateCorrectionPlan(raw []byte, ref ArtifactRef, snapshotDigest string,
	group sceneGroup, bindings map[int]v2input.SessionVersionBinding) error {
	invalid := func() error { return domain.NewError("ANALYSIS_CONFLICT", 409) }
	if !exactCanonicalJSON(raw, ref.SHA256, 8<<20) || !exactKeys(raw,
		"schema_version", "snapshot_digest", "items") {
		return invalid()
	}
	var doc struct {
		SchemaVersion  int               `json:"schema_version"`
		SnapshotDigest string            `json:"snapshot_digest"`
		Items          []json.RawMessage `json:"items"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil || decoder.Decode(new(any)) != io.EOF ||
		doc.SchemaVersion != 1 || doc.SnapshotDigest != snapshotDigest ||
		len(doc.Items) != len(group.Ordinals) {
		return invalid()
	}
	expected := make(map[int]bool, len(group.Ordinals))
	for _, ordinal := range group.Ordinals {
		expected[ordinal] = true
	}
	seen := make(map[int]bool, len(doc.Items))
	for _, rawItem := range doc.Items {
		if !exactKeys(rawItem, "binding", "ordinal", "delta", "disposition", "reason") {
			return invalid()
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(rawItem, &fields) != nil ||
			!exactKeys(fields["binding"], "asset_version_id", "source", "canvas_hash") {
			return invalid()
		}
		var bindingFields map[string]json.RawMessage
		if json.Unmarshal(fields["binding"], &bindingFields) != nil ||
			!exactKeys(bindingFields["source"], "artifact_id", "sha256") {
			return invalid()
		}
		var item struct {
			Binding     v2input.SessionVersionBinding `json:"binding"`
			Ordinal     int                           `json:"ordinal"`
			Delta       json.RawMessage               `json:"delta"`
			Disposition string                        `json:"disposition"`
			Reason      json.RawMessage               `json:"reason"`
		}
		itemDecoder := json.NewDecoder(bytes.NewReader(rawItem))
		itemDecoder.DisallowUnknownFields()
		if itemDecoder.Decode(&item) != nil || itemDecoder.Decode(new(any)) != io.EOF ||
			strconv.Itoa(item.Ordinal) != string(fields["ordinal"]) ||
			!expected[item.Ordinal] || seen[item.Ordinal] {
			return invalid()
		}
		binding, found := bindings[item.Ordinal]
		if !found || item.Binding != binding {
			return invalid()
		}
		vector, err := v2draft.DecodeCorrectionVector(item.Delta)
		if err != nil || (item.Disposition != "apply" &&
			item.Disposition != "no_change" && item.Disposition != "manual_locked") {
			return invalid()
		}
		if item.Disposition != "apply" && vector != (v2draft.CorrectionVector{}) {
			return invalid()
		}
		if string(item.Reason) != "null" {
			var reason string
			if json.Unmarshal(item.Reason, &reason) != nil || len(reason) < 1 || len(reason) > 80 {
				return invalid()
			}
		}
		seen[item.Ordinal] = true
	}
	return nil
}
