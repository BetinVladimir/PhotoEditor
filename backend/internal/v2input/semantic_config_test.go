package v2input

import (
	"bytes"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestSemanticConfigFrozenNodesOwnedAndOrdered(t *testing.T) {
	project, snapshot, session, run := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ref := func() Ref { return Ref{uuid.New(), strings.Repeat("a", 64)} }
	pins := []SemanticProtectionPin{{1, ref(), ref(), ref()}, {0, ref(), ref(), ref()}}
	config, body, digest, err := CompileSemanticDraftConfig(project, snapshot, session, run, pins)
	if err != nil {
		t.Fatal(err)
	}
	workflow, _ := WorkflowID(run)
	task, _ := TaskID(workflow, "semantic.transfer", 0)
	if config.Semantic.Nodes[0].TaskID != task || config.Semantic.Nodes[0].ParamsArtifactID != pins[1].Params.ArtifactID {
		t.Fatal("node identity/order")
	}
	expected, _ := canonical.Hash(config)
	if config.QueueClass!="batch" || digest != expected {
		t.Fatal("config digest")
	}
	again, _, _, _ := CompileSemanticDraftConfig(project, snapshot, session, run, pins)
	frozen, _ := canonical.Marshal(again)
	if !bytes.Equal(body, frozen) {
		t.Fatal("non deterministic")
	}
	pins[1].SourceProtection.SHA256 = strings.Repeat("f", 64)
	if config.Semantic.Nodes[0].SourceProtection.SHA256 != strings.Repeat("a", 64) {
		t.Fatal("borrowed pins")
	}
	pins[1].Ordinal = 1
	if _, _, _, err := CompileSemanticDraftConfig(project, snapshot, session, run, pins); err == nil {
		t.Fatal("duplicate ordinal accepted")
	}
}

func TestSemanticConfigSupportsFiveThousandNodes(t *testing.T) {
	pins := make([]SemanticProtectionPin, 5000)
	for n := range pins {
		pins[n] = SemanticProtectionPin{Ordinal: n, Params: Ref{uuid.New(), strings.Repeat("a", 64)}, SourceProtection: Ref{uuid.New(), strings.Repeat("b", 64)}, TargetProtection: Ref{uuid.New(), strings.Repeat("c", 64)}}
	}
	config, body, _, err := CompileSemanticDraftConfig(uuid.New(), uuid.New(), uuid.New(), uuid.New(), pins)
	if err != nil || len(config.Semantic.Nodes) != 5000 || len(body) > 8*1024*1024 {
		t.Fatalf("maximum config: %v", err)
	}
}
