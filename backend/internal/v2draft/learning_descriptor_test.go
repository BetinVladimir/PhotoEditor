package v2draft

import (
	"strings"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func TestDescriptorShardRequiresPinnedSnapshot(t *testing.T) {
	digest := strings.Repeat("a", 64)
	body, err := canonical.Marshal(map[string]any{
		"schema_version":  1,
		"snapshot_digest": digest,
		"frames":          []any{map[string]any{"binding": map[string]any{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	frames, err := descriptorFrames(body, digest)
	if err != nil || len(frames) != 1 {
		t.Fatal("pinned shard", err)
	}
	if _, err := descriptorFrames(body, strings.Repeat("b", 64)); err == nil {
		t.Fatal("accepted a descriptor from a different input snapshot")
	}
	missing, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "snapshot_digest": digest, "frames": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := descriptorFrames(missing, digest); err == nil {
		t.Fatal("accepted an empty descriptor shard")
	}
}
