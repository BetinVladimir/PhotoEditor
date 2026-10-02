package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type inspectedObject struct {
	ID        uuid.UUID `json:"artifact_id"`
	SHA       string    `json:"sha256"`
	Bucket    string    `json:"bucket"`
	Key       string    `json:"key"`
	Version   string    `json:"version"`
	Bytes     int64     `json:"bytes"`
	MediaType string    `json:"media_type"`
}
type IngestOutcomeReader struct {
	DB      ActivationRows
	Objects ArtifactOpener
	Bucket  string
}

// Load closes SQL rows before object IO; each JSON read is bounded and checked
// by ReadIngestMetadata. Final phase persistence must recheck canonical results.
func (r IngestOutcomeReader) Load(ctx context.Context, tenant uuid.UUID, spec IngestSpec) ([]InspectionOutcome, error) {
	invalid := errors.New("invalid ingest outcome binding")
	if r.DB == nil || r.Objects == nil || r.Bucket == "" || tenant == uuid.Nil || spec.RunID == uuid.Nil || len(spec.Items) < 1 || len(spec.Items) > 10000 {
		return nil, invalid
	}
	expected := map[int64]review.ArtifactRef{}
	for _, item := range spec.Items {
		if _, ok := expected[item.Ordinal]; ok || !validExpansionRef(item.Source) {
			return nil, invalid
		}
		expected[item.Ordinal] = item.Source
	}
	rows, err := r.DB.Query(ctx, "SELECT ordinal,verify_state,inspect_state,inspect_reason,source_id,source_sha,metadata FROM photo.ingest_inspection_outcomes($1,$2)", tenant, spec.RunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InspectionOutcome{}
	objects := []inspectedObject{}
	seen := map[int64]bool{}
	for rows.Next() {
		var o InspectionOutcome
		var source review.ArtifactRef
		var raw []byte
		var reason *string
		if err = rows.Scan(&o.Ordinal, &o.Verify, &o.Inspect, &reason, &source.ID, &source.SHA256, &raw); err != nil {
			return nil, err
		}
		_, known := expected[o.Ordinal]
		if !known || len(out) >= len(spec.Items) || seen[o.Ordinal] || expected[o.Ordinal] != source {
			return nil, invalid
		}
		seen[o.Ordinal] = true
		if reason != nil {
			o.InspectReason = *reason
		}
		var obj inspectedObject
		if o.Inspect == Succeeded {
			if len(raw) > 8192 || json.Unmarshal(raw, &obj) != nil || obj.ID == uuid.Nil || obj.Bucket != r.Bucket || obj.Key == "" || obj.Version == "" || obj.Version == "null" || obj.Bytes < 1 || obj.Bytes > 1048576 || obj.MediaType != "application/json" {
				return nil, invalid
			}
			ref := review.ArtifactRef{ID: obj.ID, SHA256: obj.SHA}
			if !validExpansionRef(ref) {
				return nil, invalid
			}
			o.MetadataRef = &ref
		} else if len(raw) > 0 && string(raw) != "null" {
			return nil, invalid
		}
		out = append(out, o)
		objects = append(objects, obj)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(out) != len(spec.Items) {
		return nil, invalid
	}
	for i := range out {
		if out[i].MetadataRef == nil {
			continue
		}
		obj := objects[i]
		reader, err := r.Objects.Open(ctx, objectstore.Object{Key: obj.Key, VersionID: obj.Version, SHA256: obj.SHA, Bytes: obj.Bytes, ContentType: obj.MediaType})
		if err != nil {
			return nil, err
		}
		limited := &io.LimitedReader{R: reader, N: obj.Bytes + 1}
		metadata, readErr := ReadIngestMetadata(ctx, limited, *out[i].MetadataRef, expected[out[i].Ordinal])
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if limited.N != 1 {
			return nil, invalid
		}
		if closeErr != nil {
			return nil, closeErr
		}
		out[i].Metadata = &metadata
	}
	return out, nil
}
