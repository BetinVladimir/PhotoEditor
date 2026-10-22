package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

func authoredMaskFixture(t *testing.T, index int) ([]byte, []byte, Ref) {
	t.Helper()
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil || index < 1 || index >= len(images) {
		t.Fatal("fixture inventory")
	}
	target, err := review.ParseWorkingImage(images[index])
	if err != nil {
		t.Fatal(err)
	}
	target.Color = review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", Encoding: "gamma24", ICCSHA256: strings.Repeat("e", 64)}
	body, err := canonical.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(target)
	if err != nil {
		t.Fatal(err)
	}
	step, err := review.LastTransformStep(target)
	if err != nil {
		t.Fatal(err)
	}
	var from struct {
		Canvas review.Canvas `json:"from_canvas"`
	}
	if json.Unmarshal(step, &from) != nil {
		t.Fatal("step")
	}
	var chain struct {
		Steps []json.RawMessage `json:"steps"`
		Hash  string            `json:"hash"`
	}
	if json.Unmarshal(target.TransformChain, &chain) != nil || len(chain.Steps) != 1 {
		t.Fatal("single-step fixture required")
	}
	prefix := struct {
		Steps []json.RawMessage `json:"steps"`
	}{Steps: []json.RawMessage{}}
	hash, err := canonical.Hash(prefix)
	if err != nil {
		t.Fatal(err)
	}
	chainBody, err := canonical.Marshal(struct {
		Steps []json.RawMessage `json:"steps"`
		Hash  string            `json:"hash"`
	}{prefix.Steps, hash})
	if err != nil {
		t.Fatal(err)
	}
	mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Canvas: from.Canvas, TransformChain: chainBody, Selector: "manual", Confidence: 1, Protected: true}
	maskBody, err := canonical.Marshal(mask)
	if err != nil {
		t.Fatal(err)
	}
	return maskBody, body, Ref{ArtifactID: uuid.New(), SHA256: sha}
}

func TestAuthoredMaskTransformAllExistingGeometryKindsAndClosure(t *testing.T) {
	for index, name := range []string{"matrix", "dense_warp", "crop", "resize"} {
		t.Run(name, func(t *testing.T) {
			mask, target, ref := authoredMaskFixture(t, index+1)
			p, inputs, err := PrepareAuthoredMaskTransformParams(context.Background(), mask, target, ref)
			if err != nil {
				t.Fatal(err)
			}
			var step struct {
				Kind string `json:"kind"`
			}
			if json.Unmarshal(p.Step, &step) != nil || step.Kind != name {
				t.Fatal("wrong actual step")
			}
			paramSHA, err := canonical.Hash(p)
			if err != nil {
				t.Fatal(err)
			}
			params := Ref{ArtifactID: uuid.New(), SHA256: paramSHA}
			entries := append(append([]Entry(nil), inputs...), Entry{params, "Params"})
			sizes := make([]SemanticInputSize, len(entries))
			for i, v := range entries {
				sizes[i] = SemanticInputSize{Ref: v.Ref, Bytes: 1024}
				if v.Ref == params {
					body, e := canonical.Marshal(p)
					if e != nil {
						t.Fatal(e)
					}
					sizes[i].Bytes = int64(len(body))
				}
				if v.Ref == ref {
					sizes[i].Bytes = int64(len(target))
				}
			}
			for _, changed := range []Ref{params, ref} {
				bad := append([]SemanticInputSize(nil), sizes...)
				for i := range bad {
					if bad[i].Ref == changed {
						bad[i].Bytes--
					}
				}
				if _, e := CompileAuthoredMaskTransformTask(context.Background(), uuid.New(), mask, target, ref, params, entries, bad, 1<<40, 1<<40, 1<<40); e == nil {
					t.Fatal("understated physical JSON accepted")
				}
			}
			frozen, err := CompileAuthoredMaskTransformTask(context.Background(), uuid.New(), mask, target, ref, params, entries, sizes, 1<<40, 1<<40, 1<<40)
			if err != nil {
				t.Fatal(err)
			}
			draft, err := frozen.DraftTask()
			if err != nil || draft.Operation != "mask.transform" || draft.OperationVersion != "1" || draft.ResourceClass != "cpu" {
				t.Fatal(draft, err)
			}
			original := frozen.profile
			entries[0].Contract = "Invented"
			sizes[0].Bytes = 1
			mask[0] = 'x'
			target[0] = 'x'
			if frozen.profile != original {
				t.Fatal("caller aliases frozen profile")
			}
			body, err := frozen.ParamsBytes()
			if err != nil {
				t.Fatal(err)
			}
			body[0] = 'x'
			owned, err := frozen.ParamsBytes()
			if err != nil || owned[0] == 'x' {
				t.Fatal("Params alias")
			}
		})
	}
}

func TestAuthoredMaskTransformRejectsSourceSubstitutions(t *testing.T) {
	for _, name := range []string{"foreign_target", "wrong_source_canvas", "nonprefix", "no_step", "unknown_mask", "noncanonical", "canceled"} {
		t.Run(name, func(t *testing.T) {
			mask, target, ref := authoredMaskFixture(t, 1)
			ctx := context.Background()
			switch name {
			case "foreign_target":
				ref.SHA256 = strings.Repeat("0", 64)
			case "wrong_source_canvas":
				var m review.MaskRef
				json.Unmarshal(mask, &m)
				m.Canvas.Width++
				mask, _ = canonical.Marshal(m)
			case "nonprefix":
				var m review.MaskRef
				json.Unmarshal(mask, &m)
				var image review.WorkingImage
				json.Unmarshal(target, &image)
				m.TransformChain = image.TransformChain
				m.Canvas = image.Canvas
				mask, _ = canonical.Marshal(m)
			case "no_step":
				var image review.WorkingImage
				json.Unmarshal(target, &image)
				var m review.MaskRef
				json.Unmarshal(mask, &m)
				image.Canvas = m.Canvas
				image.TransformChain = m.TransformChain
				target, _ = canonical.Marshal(image)
				ref.SHA256, _ = canonical.Hash(image)
			case "unknown_mask":
				mask = append([]byte(`{"invented":true,`), mask[1:]...)
			case "noncanonical":
				target = append(target, ' ')
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, _, err := PrepareAuthoredMaskTransformParams(ctx, mask, target, ref); err == nil {
				t.Fatal("substitution accepted")
			}
		})
	}
}

func TestAuthoredMaskTransformResourceBoundsAndCancellation(t *testing.T) {
	raw, err := CompileAuthoredMaskTransformProfile(400, 300, 200, 150, 4096, 1<<40, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Backend  string                  `json:"backend"`
		Index    int64                   `json:"input_index_scratch_bytes"`
		Estimate sessionDescribeEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Backend != "cpu" || p.Index != 16<<20 || p.Estimate.RAMBytes < 1<<30 || p.Estimate.VRAMBytes != 0 {
		t.Fatal("native CPU resource contract")
	}
	for _, name := range []string{"ram", "scratch", "output", "pixels", "overflow"} {
		t.Run(name, func(t *testing.T) {
			ram, scratch, output := p.Estimate.RAMBytes, p.Estimate.ScratchBytes, p.Estimate.OutputBytes
			sw := int64(400)
			input := int64(4096)
			switch name {
			case "ram":
				ram--
			case "scratch":
				scratch--
			case "output":
				output--
			case "pixels":
				sw = 200000001
			case "overflow":
				input = 9007199254740991
			}
			if _, err := CompileAuthoredMaskTransformProfile(sw, 300, 200, 150, input, ram, scratch, output); err == nil {
				t.Fatal("bound accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishAuthoredMaskTransformTask(ctx, FrozenAuthoredMaskTransformTask{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
