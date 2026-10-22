package v2render

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"math"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestAuthoredSkinAdmissionPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (AuthoredSkinPreparation{}).Admit(ctx, auth.Identity{}, AuthoredInstructionSource{}, nil, CurrentAuthoredFaceSelection{}, v2qualification.Expected{}, v2input.Ref{}, AuthoredFaceBudget{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation identity lost", err)
	}
}
func TestAuthoredSkinMeasuredCPUCostsMustFitActualProfile(t *testing.T) {
	raw, err := v2input.CompileSemanticFaceSkinProfile(12, 9, 4096, 1<<40, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	report := v2qualification.Report{Operation: "mask.face_skin", Version: "1", Metrics: v2qualification.Metrics{PeakRSSBytes: 4096, ScratchBytes: 4096}}
	if err := authoredSkinMeasuredBudget(raw, report); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"wrong-operation", "vram", "rss", "scratch", "nan", "infinite", "negative"} {
		t.Run(variant, func(t *testing.T) {
			r := report
			switch variant {
			case "wrong-operation":
				r.Operation = "face.parse"
			case "vram":
				r.Metrics.PeakVRAMBytes = 1
			case "rss":
				r.Metrics.PeakRSSBytes = 1 << 40
			case "scratch":
				r.Metrics.ScratchBytes = 1 << 40
			case "nan":
				r.Metrics.PeakRSSBytes = math.NaN()
			case "infinite":
				r.Metrics.ScratchBytes = math.Inf(1)
			case "negative":
				r.Metrics.PeakRSSBytes = -1
			}
			if err := authoredSkinMeasuredBudget(raw, r); err == nil {
				t.Fatal("unsupported measured costs accepted")
			}
		})
	}
}
func TestAuthoredSkinNamespaceCannotRelabelModelInference(t *testing.T) {
	source, _, _ := authoredFaceCandidateFixture(t)
	run, workflow, err := AuthoredSkinPrerequisiteIDs(source, "mask.face_skin")
	if err != nil || run == source.Parent || workflow == source.Workflow {
		t.Fatal("dedicated namespace", err)
	}
	if _, _, err := AuthoredSkinPrerequisiteIDs(source, "face.parse"); err == nil {
		t.Fatal("model relabel accepted")
	}
	source.Instruction.Ordinal++
	if _, _, err := AuthoredSkinPrerequisiteIDs(source, "mask.face_skin"); err == nil {
		t.Fatal("foreign instruction accepted")
	}
}

func TestAuthoredSkinReusesActualTypedCompilerAndThreeInputClosure(t *testing.T) {
	source, bodies, observations := authoredFaceCandidateFixture(t)
	// This compiler consumes the production gamma24 working-image contract.
	// Rebind this local fixture only; the shared face candidate fixture remains
	// unchanged for its broader typed-document tests.
	workingRaw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil { t.Fatal(err) }
	var working []review.WorkingImage
	var image review.WorkingImage
	if json.Unmarshal(workingRaw, &working) != nil || len(working) == 0 || json.Unmarshal(bodies[0], &image) != nil {
		t.Fatal("actual working-image color fixture")
	}
	image.Color = working[0].Color
	bodies[0], err = canonical.Marshal(image)
	if err != nil { t.Fatal(err) }
	source.Source.Source.SHA256 = authoredFaceCandidateSHA(bodies[0])
	// Rehash the entire local producer chain after the source binding changes.
	// Keep actual artifact identities, typed fields, geometry and label pixels.
	for i := 1; i < len(bodies); i++ {
		var doc map[string]any
		if json.Unmarshal(bodies[i], &doc) != nil { t.Fatal("local face document") }
		doc["image"] = source.Source.Source
		if i > 1 { doc["faces"] = observations[0].Roots[0] }
		bodies[i], err = canonical.Marshal(doc)
		if err != nil { t.Fatal(err) }
		roots := observations[i-1].Roots
		roots[len(roots)-1].SHA256 = authoredFaceCandidateSHA(bodies[i])
	}
	var labels review.FaceLabels19
	if json.Unmarshal(bodies[3], &labels) != nil {
		t.Fatal("actual labels fixture")
	}
	parserBody, err := canonical.Marshal(review.FaceParseParams{Image: labels.Image, Faces: labels.Faces, ModelID: labels.ModelID, ModelLockID: labels.ModelLockID, PreprocessingID: labels.PreprocessingID, CropSize: 512, Expansion: 1.35})
	if err != nil {
		t.Fatal(err)
	}
	parserParams := observations[2].Params
	parserParams.SHA256 = authoredFaceCandidateSHA(parserBody)
	prepared, err := v2input.PrepareSemanticFaceSkinParams(context.Background(), source.Source, bodies[0], observations[2].Roots[1], bodies[3], parserParams, parserBody, observations[0].Roots[0], bodies[1])
	if err != nil {
		t.Fatal("installed typed Params", err)
	}
	_, workflow, err := AuthoredSkinPrerequisiteIDs(source, "mask.face_skin")
	if err != nil {
		t.Fatal(err)
	}
	task, err := v2input.TaskID(workflow, "mask.face_skin", 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("params")), SHA256: authoredFaceCandidateSHA(prepared.Params)}
	sizes := []v2input.SemanticInputSize{{Ref: ref, Bytes: int64(len(prepared.Params))}}
	for _, entry := range prepared.Inputs {
		sizes = append(sizes, v2input.SemanticInputSize{Ref: entry.Ref, Bytes: 4096})
	}
	limits := AuthoredFaceBudget{Backend: "cpu", Sizes: sizes, RAM: 1 << 40, Scratch: 1 << 40, Output: 1 << 40}
	frozen, err := CompileAuthoredFaceSkin(context.Background(), source, bodies[0], observations[2].Roots[1], bodies[3], parserParams, parserBody, observations[0].Roots[0], bodies[1], ref, limits)
	if err != nil {
		t.Fatal("actual CPU compiler", err)
	}
	frozen, err = frozen.WithInputIndexScratch()
	if err != nil {
		t.Fatal(err)
	}
	draft, err := frozen.DraftTask()
	if err != nil || draft.Operation != "mask.face_skin" || draft.ResourceClass != "cpu" || draft.ID != task {
		t.Fatal("actual dispatch", err)
	}
	entries := append(append([]v2input.Entry(nil), prepared.Inputs...), v2input.Entry{Ref: ref, Contract: "Params"})
	if err = frozen.VerifyInputInventory(context.Background(), entries); err != nil {
		t.Fatal("exact complete inventory", err)
	}
	if err = frozen.VerifyInputInventory(context.Background(), entries[:2]); err == nil {
		t.Fatal("missing inventory accepted")
	}
	// The installed worker accepts full-image labels, not a copied selected-ID map.
	var params map[string]json.RawMessage
	if json.Unmarshal(prepared.Params, &params) != nil || len(params) != 2 || params["labels"] == nil || params["algorithm"] == nil {
		t.Fatal("installed mask Params changed")
	}
}

func TestAuthoredSkinIndexOverheadCannotExceedOriginalLimits(t *testing.T) {
	raw, err := v2input.CompileSemanticFaceSkinProfile(12, 9, 4096, 1<<40, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	limits := AuthoredFaceBudget{Backend: "cpu", RAM: 1 << 40, Scratch: 1 << 40, Output: 1 << 40}
	if err = authoredSkinProfileWithinLimits(raw, limits); err != nil {
		t.Fatal(err)
	}
	limits.Scratch = 1
	if err = authoredSkinProfileWithinLimits(raw, limits); err == nil {
		t.Fatal("index/operation scratch beyond admitted limit accepted")
	}
	limits.Scratch = 1 << 40
	limits.Backend = "mps"
	if err = authoredSkinProfileWithinLimits(raw, limits); err == nil {
		t.Fatal("non CPU profile limits accepted")
	}
}
