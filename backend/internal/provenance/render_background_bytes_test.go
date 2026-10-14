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

type signingBackgroundCatalogFunc func(context.Context, auth.Identity, uuid.UUID, v2draft.LayerRef) (objectstore.Object, error)

func (f signingBackgroundCatalogFunc) Lookup(c context.Context, w auth.Identity, t uuid.UUID, n v2draft.LayerRef) (objectstore.Object, error) {
	return f(c, w, t, n)
}
func TestSigningRenderBackgroundRequiresPhysicalBytesAndStableScope(t *testing.T) {
	for _, mode := range []string{"valid", "changed_bytes", "changed_version", "wrong_icc", "missing_occurrence", "missing_entry", "nil_stream", "cancelled", "repeated"} {
		t.Run(mode, func(t *testing.T) {
			raw := "background"
			pin := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest([]byte(raw))}
			icc := strings.Repeat("c", 64)
			background := v2draft.FrozenBackgroundAsset{SchemaVersion: 1, PortableAssetID: "background-1", StyleVersionID: uuid.New(), Style: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Source: pin, ExpectedBytes: int64(len(raw)), MediaType: "image/png", Suffix: ".png", WorkICCSHA: icc}
			object := objectstore.Object{Key: "background", VersionID: "version-1", Bytes: int64(len(raw)), ContentType: "image/png", SHA256: pin.SHA256}
			node := v2draft.LayerRef{Ref: pin, Contract: "StyleBackgroundAsset", ParentRef: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, ExpectedBackground: &background}
			evidence := SigningRenderClosureEvidence{inventory: v2draft.TaskInputEvidence{Entries: []v2input.Entry{{Ref: pin, Contract: "StyleBackgroundAsset"}}, Occurrences: []v2draft.OccurrenceWitness{{Node: node, Object: object}}}}
			switch mode {
			case "wrong_icc":
				background.WorkICCSHA = strings.Repeat("d", 64)
			case "missing_occurrence":
				evidence.inventory.Occurrences = nil
			case "missing_entry":
				evidence.inventory.Entries = nil
			case "repeated":
				evidence.inventory.Occurrences = append(evidence.inventory.Occurrences, evidence.inventory.Occurrences[0])
			}
			lookups, opens := 0, 0
			catalog := signingBackgroundCatalogFunc(func(context.Context, auth.Identity, uuid.UUID, v2draft.LayerRef) (objectstore.Object, error) {
				lookups++
				current := object
				if mode == "changed_version" && lookups == 2 {
					current.VersionID = "version-2"
				}
				return current, nil
			})
			objects := signingDocumentObjectsFunc(func(context.Context, objectstore.Object) (io.ReadCloser, error) {
				opens++
				if mode == "nil_stream" {
					return nil, nil
				}
				body := raw
				if mode == "changed_bytes" {
					body = "backgrounD"
				}
				return io.NopCloser(strings.NewReader(body)), nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			err := verifySigningRenderBackgroundBytes(ctx, catalog, objects, auth.Identity{}, ReadySigningExport{}, evidence, icc)
			if mode == "valid" || mode == "repeated" {
				expected := 2
				if mode == "repeated" {
					expected = 4
				}
				if err != nil || opens != 1 || lookups != expected {
					t.Fatal("valid background rejected", err, opens, lookups)
				}
			} else if err == nil {
				t.Fatal("invalid background accepted")
			}
		})
	}
}
