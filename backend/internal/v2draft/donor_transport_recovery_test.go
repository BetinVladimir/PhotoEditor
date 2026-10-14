package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRetainedDonorTransportExactInventory(t *testing.T) {
	sha := strings.Repeat("a", 64)
	entry := v2input.Entry{Contract: "Params", Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}}
	frozen := FrozenDonorTask{draft: v2input.DraftTask{ID: uuid.New()}, entries: []v2input.Entry{entry}}
	digest, _ := canonical.Hash(frozen.entries)
	manifest := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	chunk := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	for _, mode := range []string{"ok", "digest", "count", "collision", "unknown", "chunks", "null"} {
		t.Run(mode, func(t *testing.T) {
			wire := map[string]any{"manifest": manifest, "chunks": []v2input.Ref{chunk}, "count": 1, "sha256": digest}
			switch mode {
			case "digest":
				wire["sha256"] = sha
			case "count":
				wire["count"] = 2
			case "collision":
				wire["manifest"] = entry.Ref
			case "unknown":
				wire["extra"] = true
			case "chunks":
				wire["chunks"] = []v2input.Ref{chunk, manifest}
			}
			raw, _ := canonical.Marshal(wire)
			if mode == "null" {
				raw = nil
			}
			graph, err := restoreDonorRegisteredTransport(context.Background(), frozen, raw)
			if (err == nil) != (mode == "ok") {
				t.Fatal(mode, err)
			}
			if err != nil && graph.Open != nil {
				t.Fatal("partial graph")
			}
			if mode == "ok" {
				raw[0] = '!'
				graph.Direct[0] = manifest
				cursor, err := graph.Open(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				actual, ok, err := cursor.Next(context.Background())
				if err != nil || !ok || actual != entry {
					t.Fatal("borrowed cursor")
				}
				cursor2, _ := graph.Open(context.Background())
				again, ok, _ := cursor2.Next(context.Background())
				if !ok || again != entry {
					t.Fatal("cursor not reopenable")
				}
			}
		})
	}
}
