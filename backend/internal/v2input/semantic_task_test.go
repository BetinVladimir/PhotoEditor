package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestSemanticFrozenTaskInventoryAndOwnedProfile(t *testing.T) {
	ref := func() Ref { return Ref{uuid.New(), strings.Repeat("a", 64)} }
	source := SessionVersionBinding{uuid.New(), ref(), strings.Repeat("b", 64)}
	target := SessionVersionBinding{uuid.New(), ref(), strings.Repeat("c", 64)}
	intent := SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: source, ReferenceMask: ref(), TargetSnapshotID: uuid.New(), Strength: .8, PreservePersons: true}
	intentRef := ref()
	intentRef.SHA256, _ = canonical.Hash(intent)
	params := SemanticTransferParams{1, intentRef, source, target, strings.Repeat("d", 64), ref()}
	paramsRef := ref()
	paramsRef.SHA256, _ = canonical.Hash(params)
	dependencies := SemanticTransferDependencies{ReferenceWidth: 24, ReferenceHeight: 20, TargetWidth: 24, TargetHeight: 20, ReferenceGamma: true, TargetGamma: true, Reference: CaptureImageInputs{Pixels: ref()}, Target: CaptureImageInputs{Pixels: ref()}, SelectedPixels: ref(), SourceProtection: ref(), TargetProtection: ref(), SourceProtectionPixels: ref(), TargetProtectionPixels: ref()}
	profile, err := CompileSemanticTransferProfile(24, 20, 24, 20, true, true, SemanticQualifiedCosts{}, 1<<40, 1<<40, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	workflow := uuid.New()
	frozen, err := CompileSemanticTransferTask(workflow, 0, params, intent, paramsRef, dependencies, profile)
	if err != nil {
		t.Fatal(err)
	}
	intentBody, _ := canonical.Marshal(intent)
	if err := BindSemanticProposalTasks(intentRef, intentBody, []FrozenSemanticTask{frozen}); err != nil {
		t.Fatal("proposal intent binding", err)
	}
	wrongRef := intentRef
	wrongRef.ArtifactID = uuid.New()
	if err := BindSemanticProposalTasks(wrongRef, intentBody, []FrozenSemanticTask{frozen}); err == nil {
		t.Fatal("foreign intent root accepted")
	}
	wrongRef = intentRef
	wrongRef.SHA256 = strings.Repeat("f", 64)
	if err := BindSemanticProposalTasks(wrongRef, intentBody, []FrozenSemanticTask{frozen}); err == nil {
		t.Fatal("wrong intent content accepted")
	}
	if err := BindSemanticProposalTasks(intentRef, intentBody, []FrozenSemanticTask{frozen, frozen}); err == nil {
		t.Fatal("duplicate proposal ordinal accepted")
	}
	profile[0] = '!'
	draft, err := frozen.DraftTask()
	if err != nil || draft.ExecutionProfile[0] != '{' || draft.Operation != "semantic.transfer" || draft.ResourceClass != "gpu" {
		t.Fatal("frozen draft", err)
	}
	task, _ := TaskID(workflow, "semantic.transfer", 0)
	if draft.ID != task {
		t.Fatal("task id")
	}
	pin, _ := frozen.ProtectionPin()
	if pin.Params != paramsRef || pin.SourceProtection != dependencies.SourceProtection {
		t.Fatal("protection pin")
	}
	cursor, _ := frozen.closure.Open(context.Background())
	count := 0
	for {
		entry, more, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		if !validEntry(entry) {
			t.Fatal("invalid inventory")
		}
		count++
	}
	if count != 13 {
		t.Fatalf("closure count %d", count)
	}

	var execution sessionDescribeExecutionProfile
	json.Unmarshal(draft.ExecutionProfile, &execution)
	for _, field := range []string{"ram", "scratch", "output", "pixels"} {
		changed := execution
		switch field {
		case "ram":
			changed.ResourceEstimate.RAMBytes--
		case "scratch":
			changed.ResourceEstimate.ScratchBytes--
		case "output":
			changed.ResourceEstimate.OutputBytes--
		case "pixels":
			changed.ResourceEstimate.PixelCount++
		}
		altered, _ := canonical.Marshal(changed)
		if _, err := CompileSemanticTransferTask(workflow, 0, params, intent, paramsRef, dependencies, altered); err == nil {
			t.Fatal("underbudget/geometry accepted", field)
		}
	}
	altered := strings.Replace(string(draft.ExecutionProfile), `"vram_bytes":0`, `"vram_bytes":null`, 1)
	if _, err := CompileSemanticTransferTask(workflow, 0, params, intent, paramsRef, dependencies, []byte(altered)); err == nil {
		t.Fatal("null VRAM estimate")
	}
	dependencies.SelectedPixels = Ref{}
	if _, err := CompileSemanticTransferTask(workflow, 0, params, intent, paramsRef, dependencies, draft.ExecutionProfile); err == nil {
		t.Fatal("missing selected pixels")
	}
}
