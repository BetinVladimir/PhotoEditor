package provenance

import (
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSigningRenderBinaryLeavesRequireStablePhysicalContent(t *testing.T) {
	for _, mode := range []string{"valid", "changed_bytes", "changed_version", "mime", "parent", "missing_occurrence", "missing_entry", "repeated", "duplicate_entry", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			raw := "physical-array-bytes"
			pin := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest([]byte(raw))}
			object := objectstore.Object{Key: "array", VersionID: "v1", SHA256: pin.SHA256, Bytes: int64(len(raw)), ContentType: "application/x-npy"}
			node := v2draft.LayerRef{Ref: pin, Contract: "WorkingPixels", ParentRef: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}}
			evidence := SigningRenderClosureEvidence{inventory: v2draft.TaskInputEvidence{Entries: []v2input.Entry{{Ref: pin, Contract: "NpyArray"}}, Occurrences: []v2draft.OccurrenceWitness{{Node: node, Object: object}}}}
			switch mode {
			case "mime":
				object.ContentType = "application/json"
				evidence.inventory.Occurrences[0].Object = object
			case "parent":
				evidence.inventory.Occurrences[0].Node.ParentRef = v2input.Ref{}
			case "missing_occurrence":
				evidence.inventory.Occurrences = nil
			case "missing_entry":
				evidence.inventory.Entries = nil
			case "repeated":
				evidence.inventory.Occurrences = append(evidence.inventory.Occurrences, evidence.inventory.Occurrences[0])
				evidence.inventory.Occurrences[1].Node.Contract = "MaskPixels"
			case "duplicate_entry":
				evidence.inventory.Entries = append(evidence.inventory.Entries, evidence.inventory.Entries[0])
			}
			lookups, opens := 0, 0
			catalog := signingBackgroundCatalogFunc(func(context.Context, auth.Identity, uuid.UUID, v2draft.LayerRef) (objectstore.Object, error) {
				lookups++
				current := object
				if mode == "changed_version" && lookups == 2 {
					current.VersionID = "v2"
				}
				return current, nil
			})
			objects := signingDocumentObjectsFunc(func(context.Context, objectstore.Object) (io.ReadCloser, error) {
				opens++
				body := raw
				if mode == "changed_bytes" {
					body = "Physical-array-bytes"
				}
				return io.NopCloser(strings.NewReader(body)), nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			err := verifySigningRenderBinaryBytes(ctx, catalog, objects, auth.Identity{}, ReadySigningExport{}, evidence)
			if mode == "valid" || mode == "repeated" {
				expected := 2
				if mode == "repeated" {
					expected = 4
				}
				if err != nil || opens != 1 || lookups != expected {
					t.Fatal("valid bytes rejected", err, opens, lookups)
				}
			} else if err == nil {
				t.Fatal("invalid binary leaf accepted")
			}
		})
	}
}
