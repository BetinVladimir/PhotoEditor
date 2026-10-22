package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestAuthoredGeometryPreparationCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := (SQLAuthoredGeometryPreparation{}).Prepare(ctx, auth.Identity{}, uuid.New(), uuid.New(), 1)
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	_, _, e = (SQLAuthoredGeometryWitnessReader{}).Read(ctx, AuthoredGeometryPrefix{})
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestAuthoredGeometryPreparationParamsImmutableSelection(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	s := AuthoredInstructionSource{Tenant: uuid.New(), Parent: uuid.New(), Workflow: uuid.New(), ProgramSHA: strings.Repeat("b", 64), PredecessorSHA: strings.Repeat("c", 64), OriginalRecipe: ref()}
	s.Source = v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("d", 64)}
	s.Instruction.Kind = "geometry"
	s.Instruction.Ordinal = 1
	s.Instruction.GeometryLayer = new(uuid.UUID)
	*s.Instruction.GeometryLayer = uuid.New()
	var e error
	s.Instruction.Identity, e = v2input.TaskID(s.Workflow, "render.authored_geometry", 1)
	if e != nil {
		t.Fatal(e)
	}
	authored := ref()
	layer := map[string]any{"id": *s.Instruction.GeometryLayer, "enabled": true, "opacity": 1, "operation": "optical.lens", "operation_version": "1", "params": authored}
	raw, e := canonical.Marshal(map[string]any{"layers": []any{layer}})
	if e != nil {
		t.Fatal(e)
	}
	s.LayerStackJCS = string(raw)
	root, image, report, params := ref(), ref(), ref(), ref()
	profile := "{\"backend\":\"cpu\"}"
	sum := sha256.Sum256([]byte(profile))
	n := CurrentAuthoredGeometryNative{witness: AuthoredGeometryPhaseWitness{Root: root, Image: image, Report: report, Task: AuthoredPhaseTask{RunID: uuid.New(), TaskID: uuid.New(), ResultID: uuid.New(), Operation: "optical.lens", Version: "1", Attempt: 1, Fence: 1, Params: params, InputDigest: strings.Repeat("e", 64), ProfileJCS: profile, ProfileSHA: hex.EncodeToString(sum[:]), Outputs: []v2input.Ref{root}}}, documents: map[v2input.Ref][]byte{root: []byte("{}"), image: []byte("{}"), report: []byte("{}"), params: []byte("{}")}}
	n.proof, e = canonical.Marshal(map[string]any{"native": n.witness})
	if e != nil {
		t.Fatal(e)
	}
	prefix := AuthoredGeometryPrefix{Context: s, BaseReceipt: ref()}
	parallel := ref()
	good, e := compileAuthoredGeometryPreparationParams(prefix, parallel, n)
	if e != nil {
		t.Fatal(e)
	}
	if good.Params.AuthoredParams != authored || good.Params.ParallelSource != parallel || good.Params.Native.Root != root || len(good.Params.Native.Task.Outputs) != 1 {
		t.Fatal("selection drift")
	}
	for _, tc := range []string{"disabled", "foreign_operation", "duplicate_layer", "foreign_native_proof", "missing_physical_document"} {
		t.Run(tc, func(t *testing.T) {
			// Assert the exact positive fixture immediately before each hostile mutation.
			if _, e := compileAuthoredGeometryPreparationParams(prefix, parallel, n); e != nil {
				t.Fatal(e)
			}
			p := prefix
			candidate := n
			switch tc {
			case "disabled":
				l := map[string]any{}
				for k, v := range layer {
					l[k] = v
				}
				l["enabled"] = false
				r, _ := canonical.Marshal(map[string]any{"layers": []any{l}})
				p.Context.LayerStackJCS = string(r)
			case "foreign_operation":
				l := map[string]any{}
				for k, v := range layer {
					l[k] = v
				}
				l["operation"] = "image.upscale"
				r, _ := canonical.Marshal(map[string]any{"layers": []any{l}})
				p.Context.LayerStackJCS = string(r)
			case "duplicate_layer":
				r, _ := canonical.Marshal(map[string]any{"layers": []any{layer, layer}})
				p.Context.LayerStackJCS = string(r)
			case "foreign_native_proof":
				candidate.witness.Task.ResultID = uuid.New()
			case "missing_physical_document":
				candidate.documents = map[v2input.Ref][]byte{root: []byte("{}"), image: []byte("{}"), report: []byte("{}")}
			}
			if _, e := compileAuthoredGeometryPreparationParams(p, parallel, candidate); e == nil {
				t.Fatal("hostile accepted")
			}
		})
	}
}
