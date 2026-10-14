package v2draft

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestDustCandidatesChildrenRetainOrderedFrames(t *testing.T) {
	frames := make([]review.ArtifactRef, 5)
	hashes := make([]string, 5)
	for i := range frames {
		hashes[i] = strings.Repeat(string(rune('a'+i)), 64)
		frames[i] = review.ArtifactRef{ID: uuid.New(), SHA256: hashes[i]}
	}
	id, err := canonical.Hash(map[string]any{"algorithm": "sensor_dust_log_v1", "camera_id": "raw-" + strings.Repeat("f", 64), "sensor": []int64{420, 320}, "x": 12.5, "y": 18.5, "radius": 2.0, "frames": hashes})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(map[string]any{"frames": frames, "camera_id": "raw-" + strings.Repeat("f", 64), "sensor_width": 420, "sensor_height": 320, "raw_crop": map[string]int{"x": 10, "y": 10, "width": 400, "height": 300}, "algorithm": "sensor_dust_log_v1", "candidates": []map[string]any{{"id": id, "sensor_x": 12.5, "sensor_y": 18.5, "radius_sensor_px": 2.0, "observed_frames": 5, "eligible_frames": 5, "recurrence": 1.0, "decision": "pending"}}, "warnings": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	children, err := DocumentChildren(raw, "DustCandidates", parent)
	if err != nil || len(children) != 5 {
		t.Fatal("dust frame children missing", err)
	}
	for i, child := range children {
		if child.Contract != "WorkingImage" || child.Path != "Document.frames["+string(rune('0'+i))+"]" || child.Ref.ArtifactID != frames[i].ID || child.Ref.SHA256 != frames[i].SHA256 {
			t.Fatal("dust frame order or role changed", i, child)
		}
	}
}
