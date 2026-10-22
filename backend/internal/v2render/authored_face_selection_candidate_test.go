package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Synthetic metadata around existing actual typed Python wire vectors. This
// checks content/namespace only; it is not an installed qualified observation.
func authoredFaceCandidateFixture(t *testing.T) (AuthoredInstructionSource, [][]byte, []AuthoredFaceProducerObservation) {
	t.Helper()
	pin := strings.Repeat("a", 64)
	read := func(n string) map[string]any {
		raw, e := os.ReadFile("../review/testdata/" + n)
		if e != nil {
			t.Fatal(e)
		}
		var d map[string]any
		if json.Unmarshal(raw, &d) != nil {
			t.Fatal("fixture JSON")
		}
		return d
	}
	boxes := read("face-boxes-python.json")
	landmarks := read("face-landmarks-python.json")
	labels := read("face-labels19-python.json")
	var face review.Face68
	first, _ := canonical.Marshal(landmarks["landmarks"].([]any)[0])
	if json.Unmarshal(first, &face) != nil {
		t.Fatal("fixture face")
	}
	image := review.WorkingImage{Pixels: review.ArtifactRef{ID: uuid.New(), SHA256: pin}, Canvas: face.Canvas, Color: review.ColorSpace{ProfileID: "linear_srgb_v1", Encoding: "linear", ICCSHA256: pin}, DType: "float32", TransformChain: face.TransformChain}
	sourceRaw, e := canonical.Marshal(image)
	if e != nil {
		t.Fatal(e)
	}
	ref := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	canvas, _ := canonical.Hash(image.Canvas)
	parent := uuid.New()
	workflow := uuid.NewSHA1(parent, []byte("workflow"))
	instruction, _ := v2input.TaskID(workflow, "render.authored_segment", 1)
	source := AuthoredInstructionSource{Tenant: uuid.New(), Parent: parent, Workflow: workflow, ProgramSHA: pin, PredecessorSHA: pin, Source: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(sourceRaw), CanvasHash: canvas}, Instruction: AuthoredGeometryInstruction{Kind: "segment", Ordinal: 1, Identity: instruction, Layers: []uuid.UUID{uuid.New()}}}
	boxes["image"] = source.Source.Source
	boxRaw, _ := canonical.Marshal(boxes)
	boxRef := ref(boxRaw)
	landmarks["image"] = source.Source.Source
	landmarks["faces"] = boxRef
	landRaw, _ := canonical.Marshal(landmarks)
	labels["image"] = source.Source.Source
	labels["faces"] = boxRef
	labelPixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: pin}
	labels["labels"] = labelPixels
	parseRaw, _ := canonical.Marshal(labels)
	bodies := [][]byte{sourceRaw, boxRaw, landRaw, parseRaw}
	ops := []string{"face.detect", "face.landmarks", "face.parse"}
	docs := []map[string]any{boxes, landmarks, labels}
	observations := make([]AuthoredFaceProducerObservation, 3)
	for i, op := range ops {
		run, w, e := AuthoredFacePrerequisiteIDs(source, op)
		if e != nil {
			t.Fatal(e)
		}
		task, _ := v2input.TaskID(w, op, 0)
		roots := []v2input.Ref{ref(bodies[i+1])}
		if i == 0 {
			roots[0] = boxRef
		}
		if i == 2 {
			roots = append([]v2input.Ref{labelPixels}, roots...)
		}
		observations[i] = AuthoredFaceProducerObservation{RunState: "completed", Expected: v2qualification.Expected{Operation: op, Version: "1", ModelLockSHA: docs[i]["model_lock_id"].(string), Backend: docs[i]["backend"].(string)}, Operation: op, Version: "1", Run: run, Task: task, Result: uuid.New(), Attempt: 1, Fence: 1, Params: ref([]byte("{}")), InputDigest: pin, Profile: json.RawMessage(`{}`), Roots: roots}
		if warnings, ok := docs[i]["warnings"].([]any); ok && len(warnings) > 0 {
			observations[i].RunState = "waiting_review"
			observations[i].ReviewRequired = true
		}
	}
	return source, bodies, observations
}
func TestAuthoredCurrentFaceCandidateUsesActualCurrentSelectionInventory(t *testing.T) {
	source, raw, o := authoredFaceCandidateFixture(t)
	candidate, body, err := CompileAuthoredFaceSelectionCandidate(source, raw[0], raw[1], raw[2], raw[3], o[0], o[1], o[2])
	if err != nil || len(candidate.Objects) != 1 || len(body) == 0 {
		t.Fatal("real typed current candidate", err)
	}
	// No original selected-object list is accepted by this compiler.
	if candidate.Source != source.Source || candidate.PredecessorSHA != source.PredecessorSHA {
		t.Fatal("source binding lost")
	}
}
func TestAuthoredCurrentFaceCandidateRejectsForeignProducerAndStaleSource(t *testing.T) {
	for _, variant := range []string{"source", "detector-task", "landmark-result", "parser-root", "model", "backend", "noncanonical"} {
		t.Run(variant, func(t *testing.T) {
			source, raw, o := authoredFaceCandidateFixture(t)
			switch variant {
			case "source":
				source.Source.Source.ArtifactID = uuid.New()
			case "detector-task":
				o[0].Task = uuid.New()
			case "landmark-result":
				o[1].Result = uuid.Nil
			case "parser-root":
				o[2].Roots[1].SHA256 = strings.Repeat("f", 64)
			case "model":
				o[1].Expected.ModelLockSHA = strings.Repeat("e", 64)
			case "backend":
				o[2].Expected.Backend = "other"
			case "noncanonical":
				raw[1] = append(raw[1], ' ')
			}
			if _, _, err := CompileAuthoredFaceSelectionCandidate(source, raw[0], raw[1], raw[2], raw[3], o[0], o[1], o[2]); err == nil {
				t.Fatal("foreign current candidate accepted")
			}
		})
	}
}

func TestAuthoredFaceWarningsRemainReviewRequiredObservations(t *testing.T) {
	source, raw, o := authoredFaceCandidateFixture(t)
	// Retain an actual allowed warning in a canonical detector document and bind
	// its real result bit/state; no completion or selected-ID mapping is invented.
	var doc map[string]any
	if json.Unmarshal(raw[1], &doc) != nil {
		t.Fatal("fixture")
	}
	doc["warnings"] = []string{"source_srgb_clipped"}
	body, err := canonical.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	raw[1] = body
	sum := sha256.Sum256(body)
	o[0].Roots[0].SHA256 = hex.EncodeToString(sum[:])
	// Detector reference must propagate through BOTH actual downstream documents.
	for _, index := range []int{2, 3} {
		var d map[string]any
		if json.Unmarshal(raw[index], &d) != nil {
			t.Fatal("fixture")
		}
		d["faces"] = o[0].Roots[0]
		b, e := canonical.Marshal(d)
		if e != nil {
			t.Fatal(e)
		}
		raw[index] = b
		h := sha256.Sum256(b)
		n := index - 1
		o[n].Roots[len(o[n].Roots)-1].SHA256 = hex.EncodeToString(h[:])
	}
	o[0].RunState = "waiting_review"
	o[0].ReviewRequired = true
	candidate, _, err := CompileAuthoredFaceSelectionCandidate(source, raw[0], raw[1], raw[2], raw[3], o[0], o[1], o[2])
	if err != nil || !candidate.Detector.ReviewRequired || candidate.Detector.RunState != "waiting_review" {
		t.Fatal("warning observation lost", err)
	}
	for _, variant := range []string{"cleared-bit", "silent-completed", "running"} {
		t.Run(variant, func(t *testing.T) {
			bad := append([]AuthoredFaceProducerObservation{}, o...)
			switch variant {
			case "cleared-bit":
				bad[0].ReviewRequired = false
			case "silent-completed":
				bad[0].RunState = "completed"
			case "running":
				bad[0].RunState = "running"
			}
			if _, _, err := CompileAuthoredFaceSelectionCandidate(source, raw[0], raw[1], raw[2], raw[3], bad[0], bad[1], bad[2]); err == nil {
				t.Fatal("invalid warning authority accepted")
			}
		})
	}
}
