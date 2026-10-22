package semanticreview

import (
	"bytes"
	"encoding/json"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"github.com/google/uuid"
	"os"
	"testing"
)

func TestRetouchContextBuilderDeterministicAndOwnsFaceBuffers(t *testing.T) {
	read := func(name string) []byte {
		b, e := os.ReadFile("testdata/" + name)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	params, e := ParseRetouchPreviewParams(read("retouch_preview_params.json"))
	if e != nil {
		t.Fatal(e)
	}
	var original RetouchPreviewAdapterContext
	if json.Unmarshal(read("retouch_preview_context.json"), &original) != nil {
		t.Fatal("fixture")
	}
	mask := read("retouch_preview_manual_mask.json")
	ref, raw, e := BuildRetouchPreviewContext(params, original.TransferredMask, mask, original.Faces, original.Beauty)
	if e != nil {
		t.Fatal(e)
	}
	again, body, e := BuildRetouchPreviewContext(params, original.TransferredMask, mask, original.Faces, original.Beauty)
	if e != nil || again != ref || !bytes.Equal(body, raw) {
		t.Fatal("unstable context", e)
	}
	snapshot := append([]byte(nil), raw...)
	original.Faces[0].Skin[0] = 'x'
	mask[0] = 'x'
	if !bytes.Equal(snapshot, raw) {
		t.Fatal("borrowed buffers")
	}
}

func TestRetouchContextBuilderRejectsMissingFacesOrInvalidControls(t *testing.T) {
	p := retouchParamsFixture()
	for _, beauty := range []RetouchPreviewBeauty{{}, {Enabled: true, Algorithm: "other"}} {
		ref, raw, e := BuildRetouchPreviewContext(p, p.Protection, []byte("{}"), nil, beauty)
		if e == nil || raw != nil || ref.ArtifactID.String() != "00000000-0000-0000-0000-000000000000" {
			t.Fatal("invalid context escaped")
		}
	}
}

// Exercise the public builder, rather than only its lower-level binder: callers
// cannot use defaults, a second manual region, or conflicting face references.
func TestRetouchContextBuilderPreparationMatrix(t *testing.T) {
	for _, mode := range []string{"valid", "maximum_faces", "no_faces", "too_many_faces", "duplicate_face", "nil_landmarks", "nil_object", "root_alias", "conflicting_sha", "missing_skin", "invalid_skin", "wrong_mask_sha", "noncanonical_mask", "disabled", "unprotected", "negative", "above_one", "nan", "infinite", "manual_override"} {
		t.Run(mode, func(t *testing.T) {
			read := func(name string) []byte {
				b, err := os.ReadFile("testdata/" + name)
				if err != nil { t.Fatal(err) }
				return b
			}
			params, err := ParseRetouchPreviewParams(read("retouch_preview_params.json"))
			if err != nil { t.Fatal(err) }
			var original RetouchPreviewAdapterContext
			if err := json.Unmarshal(read("retouch_preview_context.json"), &original); err != nil { t.Fatal(err) }
			mask := read("retouch_preview_manual_mask.json")
			faces, beauty, transferred := original.Faces, original.Beauty, original.TransferredMask
			switch mode {
			case "maximum_faces", "too_many_faces":
				count := 64
				if mode == "too_many_faces" { count = 65 }
				faces = make([]RetouchPreviewFace, count)
				for i := range faces {
					faces[i] = original.Faces[0]
					faces[i].Landmarks.ArtifactID = uuid.New()
				}
			case "no_faces": faces = nil
			case "duplicate_face": faces = append(faces, faces[0])
			case "nil_landmarks": faces[0].Landmarks.ArtifactID = uuid.Nil
			case "nil_object": id := uuid.Nil; faces[0].ObjectID = &id
			case "root_alias": faces[0].Landmarks = params.Source.Source
			case "conflicting_sha":
				other := faces[0]
				id1, id2 := uuid.New(), uuid.New()
				faces[0].ObjectID, other.ObjectID = &id1, &id2
				other.Landmarks.SHA256 = params.Source.Source.SHA256
				faces = append(faces, other)
			case "missing_skin": faces[0].Skin = nil
			case "invalid_skin": faces[0].Skin = json.RawMessage(`{}`)
			case "wrong_mask_sha": transferred.SHA256 = params.Source.Source.SHA256
			case "noncanonical_mask":
				mask = append(mask, ' ')
				sum := sha256.Sum256(mask)
				transferred.SHA256 = hex.EncodeToString(sum[:])
			case "disabled": beauty.Enabled = false
			case "unprotected": beauty.ProtectFeatures = false
			case "negative": beauty.Smoothing = -0.01
			case "above_one": beauty.ToneEvenness = 1.01
			case "nan": beauty.TextureKeep = math.NaN()
			case "infinite": beauty.Smoothing = math.Inf(1)
			case "manual_override":
				faces[0].Manual = json.RawMessage(`{"untrusted":true}`)
				faces[0].ManualMode = false
			}
			ref, raw, err := BuildRetouchPreviewContext(params, transferred, mask, faces, beauty)
			positive := mode == "valid" || mode == "maximum_faces" || mode == "manual_override"
			if !positive {
				if err == nil || raw != nil || ref.ArtifactID != uuid.Nil { t.Fatal("invalid preparation escaped", mode, err) }
				return
			}
			if err != nil { t.Fatal("valid preparation refused", mode, err) }
			bound, _, err := BindRetouchPreviewContext(params, ref, raw, mask)
			if err != nil || len(bound.Faces) != len(faces) { t.Fatal("prepared context cannot bind", err) }
			for _, face := range bound.Faces {
				if !face.ManualMode || !bytes.Equal(face.Manual, mask) { t.Fatal("transferred mask replaced") }
			}
		})
	}
}
