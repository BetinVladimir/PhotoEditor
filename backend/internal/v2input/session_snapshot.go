package v2input

import (
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type SessionVersionBinding struct {
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	Source         Ref       `json:"source"`
	CanvasHash     string    `json:"canvas_hash"`
}

type SessionSnapshotItem struct {
	Ordinal      int                   `json:"ordinal"`
	Binding      SessionVersionBinding `json:"binding"`
	WorkingImage Ref                   `json:"working_image"`
	CapturedAt   *string               `json:"captured_at"`
	CameraHash   *string               `json:"camera_hash"`
	Recipe       Ref                   `json:"recipe"`
}

type SessionInputSnapshot struct {
	SchemaVersion  int                   `json:"schema_version"`
	SnapshotID     uuid.UUID             `json:"snapshot_id"`
	SnapshotDigest string                `json:"snapshot_digest"`
	Items          []SessionSnapshotItem `json:"items"`
}

// AnalysisSnapshotID matches freeze_v22_analysis_snapshot's UUIDv5 name.
func AnalysisSnapshotID(run uuid.UUID) (uuid.UUID, error) {
	if run == uuid.Nil {
		return uuid.Nil, ErrInvalid
	}
	return uuid.NewSHA1(run, []byte("analysis.snapshot")), nil
}

// CompileSessionInputSnapshot produces the exact document hashed by the
// session.describe worker. The digest excludes snapshot_digest itself.
func CompileSessionInputSnapshot(run uuid.UUID, items []SessionSnapshotItem) (SessionInputSnapshot, []byte, error) {
	var empty SessionInputSnapshot
	id, err := AnalysisSnapshotID(run)
	if err != nil || len(items) < 1 || len(items) > 5000 {
		return empty, nil, ErrInvalid
	}
	seen := make(map[uuid.UUID]bool, len(items))
	for ordinal, item := range items {
		if item.Ordinal != ordinal || item.Binding.AssetVersionID == uuid.Nil ||
			seen[item.Binding.AssetVersionID] || !validRef(item.Binding.Source) ||
			!digestPattern.MatchString(item.Binding.CanvasHash) ||
			item.WorkingImage != item.Binding.Source || !validRef(item.Recipe) {
			return empty, nil, ErrInvalid
		}
		if item.CameraHash != nil && !digestPattern.MatchString(*item.CameraHash) {
			return empty, nil, ErrInvalid
		}
		if item.CapturedAt != nil {
			when, err := time.Parse(time.RFC3339Nano, *item.CapturedAt)
			if err != nil || when.Location() == nil || *item.CapturedAt != when.Format(time.RFC3339Nano) {
				return empty, nil, ErrInvalid
			}
		}
		seen[item.Binding.AssetVersionID] = true
	}
	digest, err := canonical.Hash(struct {
		SchemaVersion int                   `json:"schema_version"`
		SnapshotID    uuid.UUID             `json:"snapshot_id"`
		Items         []SessionSnapshotItem `json:"items"`
	}{1, id, items})
	if err != nil {
		return empty, nil, err
	}
	snapshot := SessionInputSnapshot{SchemaVersion: 1, SnapshotID: id,
		SnapshotDigest: digest, Items: items}
	body, err := canonical.Marshal(snapshot)
	if err != nil || len(body) > 8<<20 {
		return empty, nil, ErrInvalid
	}
	return snapshot, body, nil
}
