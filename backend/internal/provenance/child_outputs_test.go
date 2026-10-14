package provenance

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSigningChildOutputsPreserveCanonicalRootBoundary(t *testing.T) {
	pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	root, child := pin(), pin()
	node := SigningExecutionNode{Outputs: []v2input.Ref{root}, Action: SigningAction{Operation: "render.compose", Version: "1"}, Children: []SigningChildOutput{{Root: root, Ref: child, Role: "working_image"}, {Root: root, Ref: pin(), Role: "changed_mask"}, {Root: root, Ref: pin(), Role: "report"}}}
	outputs, err := signingNodeContentOutputs(node)
	if err != nil || len(outputs) != 4 || outputs[0] != root || outputs[1] != child || len(node.Outputs) != 1 {
		t.Fatal("root/child boundary lost", err)
	}
	for _, mode := range []string{"operation", "root", "role", "duplicate", "alias"} {
		changed := node
		changed.Children = append([]SigningChildOutput(nil), node.Children...)
		switch mode {
		case "operation":
			changed.Action.Operation = "asset.decode"
		case "root":
			changed.Children[0].Root = pin()
		case "role":
			changed.Children[0].Role = "unknown"
		case "duplicate":
			changed.Children = append(changed.Children, changed.Children[0])
		case "alias":
			changed.Children[0].Ref = root
		}
		if _, err := signingNodeContentOutputs(changed); err == nil {
			t.Fatal("unbound child accepted", mode)
		}
	}
}
