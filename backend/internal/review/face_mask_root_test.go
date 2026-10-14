package review

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestOriginalFaceMaskRootsPythonWire(t *testing.T) {
	for _, name := range []string{"face-skin-root-python.json", "face-manual-root-python.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			var mask ProtectionMask
			var source DefectSource
			if name == "face-manual-root-python.json" {
				var root rasterizedFaceMask
				if err := decodeExact(raw, &root); err != nil {
					t.Fatal(err)
				}
				mask = root.Target
				source.Image = root.Image
			} else {
				if err := decodeExact(raw, &mask); err != nil {
					t.Fatal(err)
				}
				reportRaw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
				if err != nil {
					t.Fatal(err)
				}
				var report proposalReportDocument
				if err := decodeExact(reportRaw, &report); err != nil {
					t.Fatal(err)
				}
				source.Image = report.Params.Image
			}
			source.Canvas = mask.Canvas
			source.TransformChain = mask.TransformChain
			sum := sha256.Sum256(raw)
			ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
			if err := VerifyFaceMaskRoot(raw, ref, mask, source); err != nil {
				t.Fatal(err)
			}
			changed := mask
			changed.Confidence = .5
			if VerifyFaceMaskRoot(raw, ref, changed, source) == nil {
				t.Fatal("changed confidence accepted")
			}
			if VerifyFaceMaskRoot(append(append([]byte{}, raw...), ' '), ref, mask, source) == nil {
				t.Fatal("changed bytes accepted")
			}
			changed = mask
			changed.Protected = true
			if VerifyFaceMaskRoot(raw, ref, changed, source) == nil {
				t.Fatal("protection accepted as target")
			}
			if name == "face-manual-root-python.json" {
				var document rasterizedFaceMask
				if err := decodeExact(raw, &document); err != nil {
					t.Fatal(err)
				}
				document.Protected.ObjectID = nil
				forged, _ := canonical.Marshal(document)
				sum = sha256.Sum256(forged)
				ref.SHA256 = hex.EncodeToString(sum[:])
				if VerifyFaceMaskRoot(forged, ref, mask, source) == nil {
					t.Fatal("mixed face bindings accepted")
				}
			}
		})
	}
}
