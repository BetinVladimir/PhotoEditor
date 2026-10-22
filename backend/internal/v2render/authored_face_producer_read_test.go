package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestAuthoredFaceProducerReadsActualOrderedRoots(t *testing.T) {
	source, bodies, observations := authoredFaceCandidateFixture(t)
	for i, observation := range observations {
		root, err := authoredFaceProducerRoot(observation.Operation, observation)
		if err != nil || root != observation.Roots[len(observation.Roots)-1] {
			t.Fatal("operation root selection", err)
		}
		if err := validateAuthoredFaceProducerDocument(source, observation.Operation, observation, bodies[i+1]); err != nil {
			t.Fatal("actual canonical producer document", err)
		}
	}
	if _, _, err := CompileAuthoredFaceSelectionCandidate(source, bodies[0], bodies[1], bodies[2], bodies[3], observations[0], observations[1], observations[2]); err != nil {
		t.Fatal("actual typed candidate", err)
	}
}

func TestAuthoredParserReaderRejectsWrongRootBoundaries(t *testing.T) {
	for _, variant := range []string{"missing-map", "extra-root", "reversed", "foreign-map", "foreign-root", "duplicate", "foreign-image", "foreign-canvas", "noncanonical"} {
		t.Run(variant, func(t *testing.T) {
			source, bodies, observations := authoredFaceCandidateFixture(t)
			o := observations[2]
			raw := bodies[3]
			switch variant {
			case "missing-map":
				o.Roots = o.Roots[1:]
			case "extra-root":
				o.Roots = append(o.Roots, v2input.Ref{ArtifactID: uuid.New(), SHA256: o.Roots[0].SHA256})
			case "reversed":
				o.Roots[0], o.Roots[1] = o.Roots[1], o.Roots[0]
			case "foreign-map":
				o.Roots[0].ArtifactID = uuid.New()
			case "foreign-root":
				o.Roots[1].SHA256 = o.Roots[0].SHA256
			case "duplicate":
				o.Roots[0] = o.Roots[1]
			case "foreign-image", "foreign-canvas":
				var d map[string]any
				if json.Unmarshal(raw, &d) != nil {
					t.Fatal("fixture")
				}
				if variant == "foreign-image" {
					d["image"] = v2input.Ref{ArtifactID: uuid.New(), SHA256: source.Source.Source.SHA256}
				} else {
					d["canvas"].(map[string]any)["width"] = 17
				}
				var err error
				raw, err = canonical.Marshal(d)
				if err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(raw)
				o.Roots[1].SHA256 = hex.EncodeToString(sum[:])
			case "noncanonical":
				raw = append(raw, ' ')
				sum := sha256.Sum256(raw)
				o.Roots[1].SHA256 = hex.EncodeToString(sum[:])
			}
			if err := validateAuthoredFaceProducerDocument(source, "face.parse", o, raw); err == nil {
				t.Fatal("invalid parser root boundary accepted")
			}
		})
	}
}

func TestAuthoredFaceProducerCardinalityIsOperationSpecific(t *testing.T) {
	source, bodies, observations := authoredFaceCandidateFixture(t)
	for i := 0; i < 2; i++ {
		o := observations[i]
		o.Roots = append(o.Roots, observations[2].Roots[0])
		if err := validateAuthoredFaceProducerDocument(source, o.Operation, o, bodies[i+1]); err == nil {
			t.Fatal("non-parser accepted two roots")
		}
	}
	o := observations[2]
	o.Operation = "mask.face_skin"
	if _, err := authoredFaceProducerRoot(o.Operation, o); err == nil {
		t.Fatal("unsupported operation accepted")
	}
	o = observations[2]
	o.Version = "2"
	if _, err := authoredFaceProducerRoot(o.Operation, o); err == nil {
		t.Fatal("unsupported version accepted")
	}
}
