package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRetouchContextSharedWorkerWire(t *testing.T) {
	read := func(name string) []byte {
		b, e := os.ReadFile("testdata/" + name)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	p, e := ParseRetouchPreviewParams(read("retouch_preview_params.json"))
	if e != nil {
		t.Fatal(e)
	}
	raw := read("retouch_preview_context.json")
	mask := read("retouch_preview_manual_mask.json")
	bind := func(b []byte) (RetouchPreviewAdapterContext, []v2input.Entry, error) {
		sum := sha256.Sum256(b)
		return BindRetouchPreviewContext(p, v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, b, mask)
	}
	context, entries, e := bind(raw)
	if e != nil || len(entries) != 4 || context.Beauty.TextureKeep != .8 {
		t.Fatal("shared wire rejected", e)
	}
	for _, failure := range []string{"source", "pins", "manual_mode", "manual_bytes", "duplicate_face", "algorithm", "strength", "unknown", "whitespace", "skin_landmarks", "skin_manual_sha", "shared_landmark_sha"} {
		t.Run(failure, func(t *testing.T) {
			var c RetouchPreviewAdapterContext
			if json.Unmarshal(raw, &c) != nil {
				t.Fatal("fixture")
			}
			switch failure {
			case "source":
				c.Source.AssetVersionID = uuid.New()
			case "pins":
				c.RuntimeLockSHA = p.ModelLockSHA
			case "manual_mode":
				c.Faces[0].ManualMode = false
			case "manual_bytes":
				c.Faces[0].Manual = json.RawMessage(`null`)
			case "duplicate_face":
				c.Faces = append(c.Faces, c.Faces[0])
			case "algorithm":
				c.Algorithm = "big_lama"
			case "skin_landmarks", "skin_manual_sha":
				var skin map[string]any
				if err := json.Unmarshal(mask, &skin); err != nil { t.Fatal(err) }
				skin["selector"] = "skin"
				artifact := c.Faces[0].Landmarks
				if failure == "skin_manual_sha" {
					encoded, err := json.Marshal(skin["artifact"])
					if err != nil { t.Fatal(err) }
					if err := json.Unmarshal(encoded, &artifact); err != nil { t.Fatal(err) }
					artifact.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
				}
				skin["artifact"] = artifact
				encoded, err := canonical.Marshal(skin)
				if err != nil { t.Fatal(err) }
				c.Faces[0].Skin = encoded
			case "shared_landmark_sha":
				first, second := uuid.New(), uuid.New()
				c.Faces[0].ObjectID = &first
				other := c.Faces[0]
				other.ObjectID = &second
				other.Landmarks.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
				c.Faces = append(c.Faces, other)
			case "strength":
				c.Beauty.Smoothing = 1.1
			}
			body, e := canonical.Marshal(c)
			if e != nil {
				t.Fatal(e)
			}
			if failure == "unknown" {
				var doc map[string]any
				json.Unmarshal(body, &doc)
				doc["approved"] = true
				body, e = canonical.Marshal(doc)
				if e != nil {
					t.Fatal(e)
				}
			}
			if failure == "whitespace" {
				body = append(body, ' ')
			}
			if _, deps, e := bind(body); e == nil || deps != nil {
				t.Fatal("invalid context accepted")
			}
		})
	}
}

func TestRetouchBeautyRejectsNonfiniteControls(t *testing.T) {
	b := RetouchPreviewBeauty{Enabled: true, Algorithm: "beautyfilter_v1", Smoothing: .5, ToneEvenness: .2, TextureKeep: .8, SkinMask: "face68_conservative_v1", ProtectFeatures: true}
	for _, v := range []float64{math.NaN(), math.Inf(1), -.1, 1.1} {
		candidate := b
		candidate.TextureKeep = v
		if validRetouchBeauty(candidate) {
			t.Fatal("invalid texture")
		}
	}
}
