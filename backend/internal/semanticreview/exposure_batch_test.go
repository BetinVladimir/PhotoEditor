package semanticreview

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestApprovedExposureBatchBindsPrivateReviewAndExactCoverage(t *testing.T) {
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	child := uuid.New()
	drafts, err := CompileApprovedTaskDrafts(child, prepared, targets, request)
	if err != nil || len(drafts) != 1 {
		t.Fatal(err)
	}
	var edit ApprovedEdit
	json.Unmarshal(drafts[0].Body, &edit)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	sha := strings.Repeat("b", 64)
	input := ExposureTaskInputs{TargetID: edit.TargetID, Source: edit.Source, Reference: prepared.Intent.Reference, Width: 24, Height: 20,
		Dependencies:  v2input.ExposureDependencies{Reference: prepared.Intent.Reference.Source, Source: v2input.CaptureImageInputs{Pixels: ref()}, ReferenceImage: v2input.CaptureImageInputs{Pixels: ref()}, MaskPixels: ref(), Protection: ref(), ProtectionPixels: ref()},
		OwnerCosts:    ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20},
		Limits:        ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000},
		Qualification: ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, IssuerID: uuid.New(), Backend: "cpu"}}
	compiled, err := CompileApprovedExposureTasks(child, prepared, targets, request, []ExposureTaskInputs{input})
	if err != nil || len(compiled) != 1 {
		t.Fatal("batch", err)
	}
	node, _ := compiled[0].DraftTask()
	if node.ID != drafts[0].TaskID || node.ParamsSHA != drafts[0].Params.SHA256 {
		t.Fatal("private Params lost")
	}
	tenant, actor := uuid.New(), uuid.New()
	proof, _ := BuildDecisionProof(prepared, targets, request)
	decision := PublishedDecision{BatchID: decisionDocumentOwner(tenant, prepared.Sources.RunID, actor, proof.RequestSHA256), Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: proof.SHA256}, Proof: proof}
	published, err := PublishExposureTask(context.Background(), compiled[0], &exposureTransportPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := PrepareApprovedExposureCommitInputs(context.Background(), tenant, actor, child, prepared, targets, request, decision, []ExposureTaskInputs{input}, []PreparedExposureTask{published})
	if err != nil || len(commit.Graphs) != 1 || len(commit.Review.Nodes) != 1 || !json.Valid(commit.Nodes) {
		t.Fatal("commit inputs", err)
	}
	checkExposureInputAdmission(t, tenant, actor, child, prepared, targets, request, decision, input, published, commit)
	var transport []ExposureTransportBinding
	if json.Unmarshal(commit.Transport, &transport) != nil || len(transport) != 1 || transport[0].TaskID != node.ID || transport[0].EntryCount != published.Graph.Built.Count || transport[0].Manifest != published.Graph.Built.Manifest || transport[0].EntriesSHA != published.Graph.Built.SHA256 || len(transport[0].DirectRoots) != published.Graph.Built.Count {
		t.Fatal("transport commit projection")
	}
	var inventory []ExposureInventoryBinding
	if json.Unmarshal(commit.Inventory, &inventory) != nil || len(inventory) != 1 || inventory[0].TaskID != node.ID || len(inventory[0].Entries) != transport[0].EntryCount {
		t.Fatal("exact inventory projection")
	}
	entriesSHA, _ := canonical.Hash(inventory[0].Entries)
	if entriesSHA != transport[0].EntriesSHA {
		t.Fatal("inventory digest not bound to transport")
	}
	foundParams := false
	for ordinal, entry := range inventory[0].Entries {
		if entry.Ref != transport[0].DirectRoots[ordinal] || entry.Contract == "InputClosureChunk" || entry.Contract == "InputClosureManifest" {
			t.Fatal("ordinal/transport collision")
		}
		if entry.Ref == published.Params && entry.Contract == "Params" {
			foundParams = true
		}
	}
	if !foundParams {
		t.Fatal("inventory lost Params contract")
	}
	if _, err := PrepareApprovedExposureCommitInputs(context.Background(), tenant, actor, child, prepared, targets, request, decision, []ExposureTaskInputs{input}, nil); err == nil {
		t.Fatal("missing transport accepted")
	}
	changed := published
	changed.Graph.Built.SHA256 = strings.Repeat("c", 64)
	if result, err := PrepareApprovedExposureCommitInputs(context.Background(), tenant, actor, child, prepared, targets, request, decision, []ExposureTaskInputs{input}, []PreparedExposureTask{changed}); err == nil || result.Nodes != nil || result.Graphs != nil {
		t.Fatal("partial changed transport accepted")
	}
	wrongDecision := decision
	wrongDecision.BatchID = uuid.New()
	if result, err := PrepareApprovedExposureCommitInputs(context.Background(), tenant, actor, child, prepared, targets, request, wrongDecision, []ExposureTaskInputs{input}, []PreparedExposureTask{published}); err == nil || result.Nodes != nil {
		t.Fatal("changed review accepted")
	}
	// Mutable projections cannot replace the private source/reference evidence.
	prepared.Intent.Reference.Source = ref()
	prepared.Intent.Kind = "remove_object"
	if _, err := CompileApprovedExposureTasks(child, prepared, targets, request, []ExposureTaskInputs{input}); err != nil {
		t.Fatal("mutable projection affected compiler", err)
	}
	for index, mutate := range []func(*ExposureTaskInputs){
		func(i *ExposureTaskInputs) { i.TargetID = uuid.New() },
		func(i *ExposureTaskInputs) { i.Source.AssetVersionID = uuid.New() },
		func(i *ExposureTaskInputs) { i.Reference.Source = ref() },
		func(i *ExposureTaskInputs) { i.Dependencies.Reference = ref() },
		func(i *ExposureTaskInputs) { i.Qualification.Backend = "cuda" },
		func(i *ExposureTaskInputs) { i.Limits.OutputBytes = 1 },
	} {
		changed := input
		mutate(&changed)
		if tasks, err := CompileApprovedExposureTasks(child, prepared, targets, request, []ExposureTaskInputs{changed}); err == nil || tasks != nil {
			t.Fatalf("invalid partial batch: mutation %d", index)
		}
	}
	for _, coverage := range [][]ExposureTaskInputs{nil, {input, input}} {
		if tasks, err := CompileApprovedExposureTasks(child, prepared, targets, request, coverage); err == nil || tasks != nil {
			t.Fatal("coverage accepted")
		}
	}
	stale := request
	stale.ExpectedRevision = "999"
	if tasks, err := CompileApprovedExposureTasks(child, prepared, targets, stale, []ExposureTaskInputs{input}); err == nil || tasks != nil {
		t.Fatal("stale batch")
	}
	request.TargetDecisions[0].Decision = "reject"
	if tasks, err := CompileApprovedExposureTasks(child, prepared, targets, request, nil); err != nil || len(tasks) != 0 {
		t.Fatal("rejection generated work", err)
	}
	other, otherTargets, otherRequest := decisionProofFixture(t)
	otherDrafts, _ := CompileApprovedTaskDrafts(child, other, otherTargets, otherRequest)
	var otherEdit ApprovedEdit
	json.Unmarshal(otherDrafts[0].Body, &otherEdit)
	unsupported := input
	unsupported.TargetID, unsupported.Source, unsupported.Reference = otherEdit.TargetID, otherEdit.Source, other.Intent.Reference
	unsupported.Dependencies.Reference = other.Intent.Reference.Source
	if tasks, err := CompileApprovedExposureTasks(child, other, otherTargets, otherRequest, []ExposureTaskInputs{unsupported}); err == nil || tasks != nil {
		t.Fatal("unsupported kind accepted")
	}
}
