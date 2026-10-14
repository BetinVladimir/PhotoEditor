package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func semanticSnapshotBodyRef(body []byte) Ref {
	sum := sha256.Sum256(body)
	return Ref{uuid.New(), hex.EncodeToString(sum[:])}
}

func TestPinnedSnapshotOwnsItemsAndRequiresCompletePinnedDocument(t *testing.T) {
	source := Ref{uuid.New(), strings.Repeat("a", 64)}
	captured := "2026-10-04T12:00:00Z"
	camera := strings.Repeat("b", 64)
	items := []SessionSnapshotItem{{Ordinal: 0, Binding: SessionVersionBinding{uuid.New(), source, strings.Repeat("c", 64)}, WorkingImage: source, Recipe: Ref{uuid.New(), strings.Repeat("d", 64)}, CapturedAt: &captured, CameraHash: &camera}}
	snapshot, body, err := CompilePinnedSessionInputSnapshot(uuid.New(), items)
	if err != nil {
		t.Fatal(err)
	}
	items[0].Binding.AssetVersionID = uuid.New()
	captured = "invalid"
	camera = "invalid"
	if *snapshot.Items[0].CapturedAt != "2026-10-04T12:00:00Z" || *snapshot.Items[0].CameraHash != strings.Repeat("b", 64) {
		t.Fatal("borrowed nested pointers")
	}
	if _, err := DecodePinnedSessionInputSnapshot(semanticSnapshotBodyRef(body), body); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"digest", "missing_nullable", "unknown", "null_version", "ordinal", "source"} {
		t.Run(variant, func(t *testing.T) {
			var value map[string]any
			if json.Unmarshal(body, &value) != nil {
				t.Fatal("fixture")
			}
			item := value["items"].([]any)[0].(map[string]any)
			switch variant {
			case "digest":
				value["snapshot_digest"] = strings.Repeat("f", 64)
			case "missing_nullable":
				delete(item, "captured_at")
			case "unknown":
				item["extra"] = true
			case "null_version":
				value["schema_version"] = nil
			case "ordinal":
				item["ordinal"] = 1
			case "source":
				item["working_image"] = map[string]any{"artifact_id": uuid.New().String(), "sha256": strings.Repeat("a", 64)}
			}
			changed, err := canonical.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodePinnedSessionInputSnapshot(semanticSnapshotBodyRef(changed), changed); err == nil {
				t.Fatal("invalid snapshot accepted with fresh content pin")
			}
		})
	}
	changed := append(append([]byte(nil), body...), ' ')
	if _, err := DecodePinnedSessionInputSnapshot(semanticSnapshotBodyRef(body), changed); err == nil {
		t.Fatal("changed pinned bytes")
	}
}
