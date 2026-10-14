package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestSemanticPinnedDocumentsCompilePublishAndFreezeConfig(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	json.Unmarshal(raw, &fixtures)
	body := fixtures[0]
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	canvas, _ := canonical.Hash(image.Canvas)
	ref := func() Ref { return Ref{uuid.New(), strings.Repeat("a", 64)} }
	doc := func(value any) (Ref, json.RawMessage) {
		body, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := canonical.Hash(value)
		if err != nil {
			t.Fatal(err)
		}
		return Ref{uuid.New(), hash}, body
	}
	sourceRef, _ := doc(image)
	targetRef, _ := doc(image)
	source := SessionVersionBinding{uuid.New(), sourceRef, canvas}
	target := SessionVersionBinding{uuid.New(), targetRef, canvas}
	// Canonical source bodies are pinned, not the fixture's arbitrary formatting.
	_, imageBody := doc(image)
	mask := func(protected bool) (Ref, json.RawMessage, Ref) {
		pixels := ref()
		value := review.MaskRef{Artifact: review.ArtifactRef{ID: pixels.ArtifactID, SHA256: pixels.SHA256}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1, Protected: protected}
		r, b := doc(value)
		return r, b, pixels
	}
	selected, selectedBody, selectedPixels := mask(false)
	sourceProtection, sourceProtectionBody, sourcePixels := mask(true)
	targetProtection, targetProtectionBody, targetPixels := mask(true)
	intent := SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: source, ReferenceMask: selected, TargetSnapshotID: uuid.New(), Strength: .8, PreservePersons: true}
	intentRef, _ := doc(intent)
	params := SemanticTransferParams{1, intentRef, source, target, strings.Repeat("b", 64), ref()}
	paramsRef, _ := doc(params)
	profile, err := CompileSemanticTransferProfile(int64(image.Canvas.Width), int64(image.Canvas.Height), int64(image.Canvas.Width), int64(image.Canvas.Height), true, true, SemanticQualifiedCosts{}, 32<<30, 12<<30, 32<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	run := uuid.New()
	workflow, _ := WorkflowID(run)
	dependencies := SemanticTransferDependencies{SourceProtection: sourceProtection, TargetProtection: targetProtection,
		// These are deliberately wrong; document-backed compilation derives children.
		SelectedPixels: ref(), SourceProtectionPixels: ref(), TargetProtectionPixels: ref()}
	documents := SemanticPinnedDocuments{Reference: imageBody, Target: imageBody, SelectedMask: selectedBody, SourceProtection: sourceProtectionBody, TargetProtection: targetProtectionBody}
	frozen, err := CompileSemanticTransferTaskFromDocuments(workflow, 0, params, intent, paramsRef, dependencies, documents, profile)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &memoryPublisher{bodies: map[uuid.UUID][]byte{}}
	prepared, err := PublishSemanticTransferTask(context.Background(), frozen, publisher)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if json.Unmarshal(publisher.bodies[prepared.Graph.Built.Manifest.ArtifactID], &manifest) != nil {
		t.Fatal("manifest")
	}
	if manifest.TaskID != prepared.Graph.ID || manifest.EntryCount != prepared.Graph.Built.Count || len(manifest.Chunks) != 1 {
		t.Fatal("transport binding/count")
	}
	pin, _ := frozen.ProtectionPin()
	config, _, _, err := CompileSemanticDraftConfig(uuid.New(), intent.TargetSnapshotID, uuid.New(), run, []SemanticProtectionPin{pin})
	if err != nil {
		t.Fatal(err)
	}
	if config.Semantic.Nodes[0].TaskID != prepared.Graph.ID || config.Semantic.Nodes[0].ParamsSHA != paramsRef.SHA256 {
		t.Fatal("frozen node mismatch")
	}
	snapshot, snapshotBody, err := CompilePinnedSessionInputSnapshot(intent.TargetSnapshotID, []SessionSnapshotItem{{Ordinal: 0, Binding: params.Target, WorkingImage: params.Target.Source, Recipe: params.Intent}})
	if err != nil {
		t.Fatal(err)
	}
	snapshotRef := semanticSnapshotBodyRef(snapshotBody)
	pinned, _, _, err := CompileSemanticSnapshotConfig(config.ProjectID, config.Semantic.AuthSessionID, run, snapshotRef, snapshotBody, []FrozenSemanticTask{frozen})
	if err != nil || pinned.Semantic.Snapshot == nil || *pinned.Semantic.Snapshot != snapshotRef || pinned.Semantic.SnapshotDigest != snapshot.SnapshotDigest {
		t.Fatalf("snapshot config: %v", err)
	}

	qualification := SemanticQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: strings.Repeat("1", 64), Report: Ref{uuid.New(), strings.Repeat("2", 64)}, Calibration: params.Calibration, ModelLockSHA: params.ModelLockSHA, RuntimeLockSHA: strings.Repeat("3", 64), DeviceProfileSHA: strings.Repeat("4", 64), DatasetSHA: strings.Repeat("5", 64), IssuerID: uuid.New(), Backend: "cuda"}
	qualified, qualifiedBody, qualifiedSHA, err := CompileQualifiedSemanticSnapshotConfig(config.ProjectID, config.Semantic.AuthSessionID, run, snapshotRef, snapshotBody, []FrozenSemanticTask{frozen}, qualification)
	if err != nil || qualified.Semantic.Qualification == nil || *qualified.Semantic.Qualification != qualification || qualifiedSHA == "" || len(qualifiedBody) == 0 {
		t.Fatal("qualified config binding", err)
	}
	qualification.PolicySHA = strings.Repeat("6", 64)
	if qualified.Semantic.Qualification.PolicySHA == qualification.PolicySHA {
		t.Fatal("borrowed qualification pin")
	}
	for _, field := range []string{"model", "calibration", "backend", "policy", "report"} {
		bad := qualification
		switch field {
		case "model":
			bad.ModelLockSHA = strings.Repeat("f", 64)
		case "calibration":
			bad.Calibration.ArtifactID = uuid.New()
		case "backend":
			bad.Backend = "cpu"
		case "policy":
			bad.PolicyID = uuid.Nil
		case "report":
			bad.Report.ArtifactID = bad.Calibration.ArtifactID
		}
		if _, _, _, err := CompileQualifiedSemanticSnapshotConfig(config.ProjectID, config.Semantic.AuthSessionID, run, snapshotRef, snapshotBody, []FrozenSemanticTask{frozen}, bad); err == nil {
			t.Fatal("bad qualification pin admitted", field)
		}
	}
	if _, _, _, err := CompileSemanticSnapshotConfig(config.ProjectID, config.Semantic.AuthSessionID, uuid.New(), snapshotRef, snapshotBody, []FrozenSemanticTask{frozen}); err == nil {
		t.Fatal("task from another run admitted")
	}
	altered := frozen
	altered.snapshotID = uuid.New()
	if _, _, _, err := CompileSemanticSnapshotConfig(config.ProjectID, config.Semantic.AuthSessionID, run, snapshotRef, snapshotBody, []FrozenSemanticTask{altered}); err == nil {
		t.Fatal("task intent snapshot changed")
	}
	changedItems := append([]SessionSnapshotItem(nil), snapshot.Items...)
	changedItems[0].Binding.AssetVersionID = uuid.New()
	_, changedBody, err := CompilePinnedSessionInputSnapshot(snapshot.SnapshotID, changedItems)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := CompileSemanticSnapshotConfig(config.ProjectID, config.Semantic.AuthSessionID, run, semanticSnapshotBodyRef(changedBody), changedBody, []FrozenSemanticTask{frozen}); err == nil {
		t.Fatal("different snapshot target admitted")
	}
	cursor, _ := prepared.Graph.Open(context.Background())
	seen := map[Ref]string{}
	for {
		entry, more, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		seen[entry.Ref] = entry.Contract
	}
	for _, pixels := range []Ref{selectedPixels, sourcePixels, targetPixels} {
		if seen[pixels] != "NpyArray" {
			t.Fatal("missing derived mask pixels")
		}
	}
	if _, ok := seen[dependencies.SelectedPixels]; ok {
		t.Fatal("caller supplied mask pixels retained")
	}

	if len(seen) != manifest.EntryCount {
		t.Fatal("inventory coverage")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	emptyPublisher := &memoryPublisher{bodies: map[uuid.UUID][]byte{}}
	if _, err := PublishSemanticTransferTask(cancelled, frozen, emptyPublisher); !errors.Is(err, context.Canceled) || len(emptyPublisher.bodies) != 0 {
		t.Fatal("cancelled publication", err)
	}
	badPublisher := &memoryPublisher{bodies: map[uuid.UUID][]byte{}, bad: true}
	if _, err := PublishSemanticTransferTask(context.Background(), frozen, badPublisher); err == nil {
		t.Fatal("unverified transport digest")
	}
	if seen[sourceProtection] != "MaskRef" || seen[targetProtection] != "MaskRef" {
		t.Fatal("protection contract")
	}
}
