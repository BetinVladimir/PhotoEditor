package provenance

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func signingGraphFixture() SigningExecutionGraph {
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	rendered := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	encoded := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	render, encode := uuid.New(), uuid.New()
	review := uuid.New()
	return SigningExecutionGraph{FinalTask: encode, Export: encoded, Sources: []v2input.Ref{source}, Nodes: []SigningExecutionNode{
		{Task: render, Run: uuid.New(), CanonicalResult: uuid.New(), Attempt: 1, Fence: 1, Inputs: []v2input.Ref{source}, Outputs: []v2input.Ref{rendered}, Action: SigningAction{Operation: "render", Version: "1", ParamsSHA: sha, Generated: true, ReviewID: &review}},
		{Task: encode, Run: uuid.New(), CanonicalResult: uuid.New(), Attempt: 1, Fence: 1, Parents: []uuid.UUID{render}, Inputs: []v2input.Ref{rendered}, Outputs: []v2input.Ref{encoded}, Action: SigningAction{Operation: "export.encode", Version: "1", ParamsSHA: sha}},
	}}
}
func TestSigningGraphPreservesDependencyOrderAndClaims(t *testing.T) {
	graph := signingGraphFixture()
	body, err := CompileSigningLineage(context.Background(), graph)
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := DecodeSigningLineage(body, v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(body)})
	if err != nil || len(lineage.Actions) != 2 || lineage.Actions[0].Operation != "render" || lineage.Actions[1].Operation != "export.encode" || !lineage.Actions[0].Generated || *lineage.Actions[0].ReviewID != *graph.Nodes[0].Action.ReviewID {
		t.Fatal("ordered claims lost", err)
	}
	graph.Nodes[0], graph.Nodes[1] = graph.Nodes[1], graph.Nodes[0]
	shuffled, err := CompileSigningLineage(context.Background(), graph)
	if err != nil || !bytes.Equal(body, shuffled) {
		t.Fatal("node storage order changed lineage", err)
	}
}
func TestSigningGraphRejectsIncompleteOrSubstitutedExecution(t *testing.T) {
	for _, mode := range []string{"output", "input", "parent", "duplicate", "duplicate_result", "cycle", "unreachable", "unused_source", "missing_source", "claim", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			graph := signingGraphFixture()
			ctx := context.Background()
			switch mode {
			case "output":
				graph.Export.ArtifactID = uuid.New()
			case "input":
				graph.Nodes[1].Inputs[0].SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			case "parent":
				graph.Nodes[1].Parents = nil
			case "duplicate":
				graph.Nodes = append(graph.Nodes, graph.Nodes[0])
			case "duplicate_result":
				graph.Nodes[1].CanonicalResult = graph.Nodes[0].CanonicalResult
			case "cycle":
				graph.Nodes[0].Parents = []uuid.UUID{graph.Nodes[1].Task}
				graph.Nodes[0].Inputs = append(graph.Nodes[0].Inputs, graph.Export)
			case "unreachable":
				extra := graph.Nodes[0]
				extra.Task = uuid.New()
				extra.Outputs = []v2input.Ref{{ArtifactID: uuid.New(), SHA256: graph.Export.SHA256}}
				graph.Nodes = append(graph.Nodes, extra)
			case "unused_source":
				graph.Sources = append(graph.Sources, v2input.Ref{ArtifactID: uuid.New(), SHA256: graph.Export.SHA256})
			case "missing_source":
				graph.Sources[0].ArtifactID = uuid.New()
			case "claim":
				graph.Nodes[0].Action.ParamsSHA = "invalid"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if body, err := CompileSigningLineage(ctx, graph); err == nil || body != nil {
				t.Fatal("invalid graph exposed lineage")
			}
		})
	}
}

func TestSigningGraphConnectsAdoptedRenderChildToExport(t *testing.T) {
	for _, mode := range []string{"valid", "child_sha", "child_root", "child_parent", "missing_child", "missing_report", "child_as_source"} {
		t.Run(mode, func(t *testing.T) {
			graph := signingGraphFixture()
			render := &graph.Nodes[0]
			render.Action.Operation = "render.compose"
			root := v2input.Ref{ArtifactID: uuid.New(), SHA256: graph.Export.SHA256}
			image := render.Outputs[0]
			render.Outputs = []v2input.Ref{root}
			render.Children = []SigningChildOutput{{Root: root, Ref: image, Role: "working_image"}, {Root: root, Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: image.SHA256}, Role: "changed_mask"}, {Root: root, Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: image.SHA256}, Role: "report"}}
			switch mode {
			case "child_sha":
				render.Children[0].Ref.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			case "child_root":
				render.Children[0].Root.ArtifactID = uuid.New()
			case "child_parent":
				graph.Nodes[1].Parents = nil
			case "missing_child":
				render.Children = nil
			case "missing_report":
				render.Children = render.Children[:2]
			case "child_as_source":
				graph.Sources = append(graph.Sources, image)
			}
			body, err := CompileSigningLineage(context.Background(), graph)
			if mode != "valid" {
				if err == nil || body != nil {
					t.Fatal("invalid child graph accepted", mode)
				}
				return
			}
			if err != nil {
				t.Fatal("valid adopted-child ancestry refused", err)
			}
			lineage, err := DecodeSigningLineage(body, v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(body)})
			if err != nil || len(lineage.Actions) != 2 || lineage.Actions[0].Operation != "render.compose" || lineage.Actions[1].Operation != "export.encode" {
				t.Fatal("render ancestry omitted", err)
			}
			graph.Nodes[0], graph.Nodes[1] = graph.Nodes[1], graph.Nodes[0]
			reordered, err := CompileSigningLineage(context.Background(), graph)
			if err != nil || !bytes.Equal(body, reordered) {
				t.Fatal("storage order changed adopted-child lineage", err)
			}
		})
	}
}
