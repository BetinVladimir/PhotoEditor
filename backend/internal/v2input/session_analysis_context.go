package v2input

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type sessionAnalysisPreflight struct {
	SessionID            uuid.UUID                `json:"session_id"`
	ProjectID            uuid.UUID                `json:"project_id"`
	SessionRevision      string                   `json:"session_revision"`
	CollectionID         uuid.UUID                `json:"collection_id"`
	SelectionSnapshotID  uuid.UUID                `json:"selection_snapshot_id"`
	SelectionDigest      string                   `json:"selection_digest"`
	ItemCount            string                   `json:"item_count"`
	ManualReferences     []sessionManualReference `json:"manual_references"`
	FrozenSnapshotID     uuid.UUID                `json:"frozen_snapshot_id"`
	FrozenSnapshotDigest string                   `json:"frozen_snapshot_digest"`
}

type sessionPlanContext struct {
	SchemaVersion        int                      `json:"schema_version"`
	SnapshotID           uuid.UUID                `json:"snapshot_id"`
	SnapshotDigest       string                   `json:"snapshot_digest"`
	ManualReferences     []sessionManualReference `json:"manual_references"`
	ManualLockedOrdinals []int                    `json:"manual_locked_ordinals"`
}

// CompileSessionPlanContext converts the freeze transaction's preflight
// evidence into the exact immutable worker context. Manual locks are derived
// from selected ordinals; the caller cannot supply them independently.
func CompileSessionPlanContext(frozen []byte,
	snapshot SessionInputSnapshot) ([]byte, string, uuid.UUID, error) {
	if !exactJSONKeys(frozen, "session_id", "project_id", "session_revision",
		"collection_id", "selection_snapshot_id", "selection_digest",
		"item_count", "manual_references", "frozen_snapshot_id",
		"frozen_snapshot_digest") {
		return nil, "", uuid.Nil, ErrInvalid
	}
	var input sessionAnalysisPreflight
	decoder := json.NewDecoder(bytes.NewReader(frozen))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF ||
		input.SessionID == uuid.Nil || input.ProjectID == uuid.Nil ||
		input.CollectionID == uuid.Nil || input.SelectionSnapshotID == uuid.Nil ||
		!digestPattern.MatchString(input.SelectionDigest) ||
		input.FrozenSnapshotID != snapshot.SnapshotID ||
		input.FrozenSnapshotDigest != snapshot.SnapshotDigest ||
		input.ManualReferences == nil {
		return nil, "", uuid.Nil, ErrInvalid
	}
	count, err := strconv.Atoi(input.ItemCount)
	if err != nil || count != len(snapshot.Items) || count < 1 || count > 5000 {
		return nil, "", uuid.Nil, ErrInvalid
	}
	revision, err := strconv.ParseInt(input.SessionRevision, 10, 64)
	if err != nil || revision < 1 || strconv.FormatInt(revision, 10) != input.SessionRevision {
		return nil, "", uuid.Nil, ErrInvalid
	}
	ordinals := make(map[uuid.UUID]int, len(snapshot.Items))
	for i, item := range snapshot.Items {
		if item.Ordinal != i || item.Binding.AssetVersionID == uuid.Nil {
			return nil, "", uuid.Nil, ErrInvalid
		}
		ordinals[item.Binding.AssetVersionID] = i
	}
	locked := make([]int, 0, len(input.ManualReferences))
	seen := make(map[uuid.UUID]bool, len(input.ManualReferences))
	for _, choice := range input.ManualReferences {
		ordinal, found := ordinals[choice.AssetVersionID]
		if !found || seen[choice.AssetVersionID] || len(choice.PreviousMembers) < 1 ||
			len(choice.PreviousMembers) > 5000 {
			return nil, "", uuid.Nil, ErrInvalid
		}
		seen[choice.AssetVersionID] = true
		members := make(map[uuid.UUID]bool, len(choice.PreviousMembers))
		for _, member := range choice.PreviousMembers {
			if _, selected := ordinals[member]; !selected || members[member] {
				return nil, "", uuid.Nil, ErrInvalid
			}
			members[member] = true
		}
		if !members[choice.AssetVersionID] {
			return nil, "", uuid.Nil, ErrInvalid
		}
		locked = append(locked, ordinal)
	}
	// SQL may return manual references in request order, while the context
	// contract requires ordinal-ordered locks.
	sort.Ints(locked)
	context := sessionPlanContext{1, snapshot.SnapshotID, snapshot.SnapshotDigest,
		input.ManualReferences, locked}
	body, err := canonical.Marshal(context)
	if err != nil || len(body) > 1048576 ||
		validateSessionPlanContext(body, snapshot) != nil {
		return nil, "", uuid.Nil, ErrInvalid
	}
	sha, err := canonical.Hash(json.RawMessage(body))
	if err != nil {
		return nil, "", uuid.Nil, err
	}
	return body, sha, input.ProjectID, nil
}
