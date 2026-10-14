package semanticproposal

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
)

func TestTransferWorkflowFromPinnedDocuments(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	pin := func(body []byte) v2input.Ref {
		sum := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	canvas, _ := canonical.Hash(image.Canvas)
	source := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: pin(images[0]), CanvasHash: canvas}
	target := source
	target.AssetVersionID = uuid.New()
	mask := func(protected bool) (v2input.Ref, json.RawMessage) {
		m := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1, Protected: protected}
		m.Artifact.ID = uuid.New()
		body, err := canonical.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return pin(body), body
	}
	selected, selectedBody := mask(false)
	sourceProtection, sourceBody := mask(true)
	targetProtection, targetBody := mask(true)
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: source, ReferenceMask: selected, TargetSnapshotID: uuid.New(), Strength: .5, PreservePersons: true}
	body, _ := canonical.Marshal(intent)
	request, err := CompileRequest(uuid.New(), uuid.New(), "document-plan", body)
	if err != nil {
		t.Fatal(err)
	}
	published := PublishedIntent{Ref: pin(body), Inventory: CatalogInventory{Scope: CatalogScope{ProjectID: uuid.New(), ItemCount: 1}, Targets: []v2input.SessionVersionBinding{target}}}
	qualification := v2input.SemanticQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: strings.Repeat("b", 64), Report: ref(), Calibration: ref(), ModelLockSHA: strings.Repeat("c", 64), RuntimeLockSHA: strings.Repeat("d", 64), DeviceProfileSHA: strings.Repeat("e", 64), DatasetSHA: strings.Repeat("f", 64), IssuerID: uuid.New(), Backend: "cuda"}
	snapshot, snapshotBody, err := v2input.CompilePinnedSessionInputSnapshot(intent.TargetSnapshotID, []v2input.SessionSnapshotItem{{Ordinal: 0, Binding: target, WorkingImage: target.Source, Recipe: ref()}})
	if err != nil {
		t.Fatal(err)
	}
	snapshotRef := ref()
	snapshotRef.SHA256, _ = canonical.Hash(snapshot)
	params := v2input.SemanticTransferParams{SchemaVersion: 1, Intent: published.Ref, Reference: source, Target: target, ModelLockSHA: qualification.ModelLockSHA, Calibration: qualification.Calibration}
	paramsBody, _ := canonical.Marshal(params)
	paramsRef := pin(paramsBody)
	profile, err := v2input.CompileSemanticTransferProfile(400, 300, 400, 300, true, true, v2input.SemanticQualifiedCosts{}, 1<<30, 1<<30, 1<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []TransferTaskInputs{{Dependencies: v2input.SemanticTransferDependencies{SourceProtection: sourceProtection, TargetProtection: targetProtection}, Profile: profile}}
	documents := []v2input.SemanticPinnedDocuments{{Reference: images[0], Target: images[0], SelectedMask: selectedBody, SourceProtection: sourceBody, TargetProtection: targetBody}}
	session := uuid.New()
	plan, err := CompileTransferWorkflowPlanFromDocuments(request, session, published, snapshotRef, snapshotBody, []v2input.Ref{paramsRef}, inputs, qualification, documents)
	if err != nil || len(plan.DraftTasks()) != 1 {
		t.Fatal("document workflow", err)
	}
	documents[0].SelectedMask = append(append(json.RawMessage(nil), selectedBody...), ' ')
	if _, err := CompileTransferWorkflowPlanFromDocuments(request, session, published, snapshotRef, snapshotBody, []v2input.Ref{paramsRef}, inputs, qualification, documents); err == nil {
		t.Fatal("changed mask bytes accepted")
	}
}
