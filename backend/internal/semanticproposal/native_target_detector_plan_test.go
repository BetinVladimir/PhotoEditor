package semanticproposal

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Extends the existing RGB/alpha/geometry catalog fixture with a distinct target
// namespace. This is compiler evidence, not current DB/source authorization.
func assertNativeTargetDetectorPlan(t *testing.T, reader SQLTransferSourceReader, catalog *detectorCatalogFixture, who auth.Identity, reference detectorDraftPlan, image []byte, params v2input.Ref) {
	t.Helper()
	ctx := context.Background()
	intent := reference.request.Intent
	intent.Reference.AssetVersionID = uuid.New() // original reference outside target inventory
	raw, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	request, err := CompileRequest(reference.request.TenantID, who.UserID, "target-detector", raw)
	if err != nil {
		t.Fatal(err)
	}
	published := reference.published
	published.Ref.ArtifactID = uuid.New()
	published.Ref.SHA256, err = canonical.Hash(intent)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := compileNativeTargetPreparationManifest(ctx, who, request, "target-detector", published, reference.anchor.snapshotRef, reference.anchor.snapshotBody)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := compileNativeTargetAnchor(ctx, who, request, "target-detector", published, reference.anchor.snapshotRef, reference.anchor.snapshotBody, manifest, 0, "face.detect")
	if err != nil {
		t.Fatal(err)
	}
	_, workflow, err := detectorPreparationIDs(anchor.entry.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := selectQualificationPolicies(reference.policy, []string{"face.detect"})
	if err != nil {
		t.Fatal(err)
	}
	policy := expected["face.detect"]
	child, err := prepareCatalogDetectorTask(ctx, catalog, who, request.TenantID, workflow, 0, anchor.entry.Binding, image, params, policy.ModelLockSHA, .8, 64, reference.prepared.costs, 1<<40, 12<<30, 1<<40, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	legacy := child
	child.frozen, err = child.frozen.WithExecutionIdentity(policy.ModelLockSHA, policy.RuntimeLockSHA, policy.DeviceProfileSHA)
	if err != nil {
		t.Fatal(err)
	}
	task, err := child.frozen.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	prepared := reference.prepared
	prepared.parent = anchor.entry.Namespace
	prepared.document = uuid.NewSHA1(anchor.entry.Namespace, []byte("semantic.face.detect.params:reference"))
	prepared.frozen = child.frozen
	prepared.inputs = child.inputs
	prepared.qualification.taskSHA, err = canonical.Hash(task)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := reader.compileNativeTargetDetectorPlan(ctx, who, anchor, prepared, reference.policy, 16<<20)
	if err != nil {
		t.Fatal("target detector compiler", err)
	}
	var queueConfig map[string]json.RawMessage
	if json.Unmarshal(plan.detector.shell.Config, &queueConfig) != nil || string(queueConfig["queue_class"]) != `"batch"` { t.Fatal("target detector queue missing") }
	oldPrepared := prepared
	oldPrepared.legacyTargetQueueOmitted = true
	oldPlan, err := reader.compileNativeTargetDetectorPlan(ctx, who, anchor, oldPrepared, reference.policy, 16<<20)
	if err != nil { t.Fatal("historical target detector compile", err) }
	var oldConfig map[string]json.RawMessage
	if json.Unmarshal(oldPlan.detector.shell.Config, &oldConfig) != nil { t.Fatal("historical target detector config") }
	if _, present := oldConfig["queue_class"]; present { t.Fatal("historical target detector queue fabricated") }
	oldReplay, err := reader.compileNativeTargetDetectorPlan(ctx, who, anchor, oldPlan.detector.prepared, reference.policy, 16<<20)
	if err != nil || oldReplay.seal != oldPlan.seal || string(oldReplay.detector.shell.Config) != string(oldPlan.detector.shell.Config) { t.Fatal("historical target detector replay changed", err) }
	if plan.detector.shell.Run == reference.shell.Run || plan.detector.shell.Run == request.RunID || plan.detector.shell.Tasks[0].ID != task.ID || plan.detector.request.RequestSHA != request.RequestSHA || plan.detector.request.Intent.Reference != intent.Reference || plan.detector.prepared.source.binding != anchor.entry.Binding {
		t.Fatal("target/original identities conflated")
	}
	var config struct {
		Target struct {
			Parent uuid.UUID                     `json:"parent_run_id"`
			SHA    string                        `json:"request_sha"`
			Source v2input.SessionVersionBinding `json:"source"`
			Stage  uuid.UUID                     `json:"target_stage_id"`
		} `json:"v27_target_detector_prepare"`
	}
	if json.Unmarshal(plan.detector.shell.Config, &config) != nil || config.Target.Parent != request.RunID || config.Target.SHA != request.RequestSHA || config.Target.Source != anchor.entry.Binding || config.Target.Stage != anchor.stage.ID {
		t.Fatal("target config lost original lineage")
	}
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	replay, err := reader.compileNativeTargetDetectorPlan(ctx, who, anchor, prepared, reference.policy, 16<<20)
	if err != nil || replay.seal != plan.seal {
		t.Fatal("target replay changed", err)
	}

	publisher := &proposalMemoryTransport{}
	transport, packet, err := publishNativeTargetDetectorTransport(ctx, plan, publisher)
	if err != nil || transport.Graph.ID != task.ID || packet.Count != transport.Graph.Built.Count || packet.EntriesSHA != transport.Graph.Built.SHA256 || publisher.calls < 2 {
		t.Fatal("target transport publication", err)
	}
	transportAgain, _, err := publishNativeTargetDetectorTransport(ctx, plan, &proposalMemoryTransport{})
	if err != nil || transportAgain.Graph.Built.Manifest != transport.Graph.Built.Manifest {
		t.Fatal("target transport replay changed", err)
	}
	if _, _, err := publishNativeTargetDetectorTransport(ctx, plan, &proposalMemoryTransport{failAt: 1}); err == nil {
		t.Fatal("target publication failure hidden")
	}
	canceledTransport, cancelTransport := context.WithCancel(ctx)
	cancelTransport()
	untouched := &proposalMemoryTransport{}
	if _, _, err := publishNativeTargetDetectorTransport(canceledTransport, plan, untouched); err == nil || untouched.calls != 0 {
		t.Fatal("canceled target publisher wrote bytes")
	}
	// Source-wide legacy plans are not accepted in the new target execution path.
	legacyPrepared := prepared
	legacyPrepared.frozen = legacy.frozen
	legacyPrepared.inputs = legacy.inputs
	legacyTask, _ := legacy.frozen.DraftTask()
	legacyPrepared.qualification.taskSHA, _ = canonical.Hash(legacyTask)
	if _, err := reader.compileNativeTargetDetectorPlan(ctx, who, anchor, legacyPrepared, reference.policy, 16<<20); err == nil {
		t.Fatal("identity-free target profile accepted")
	}
	for _, kind := range []string{"parent", "document", "binding", "report", "policy", "scope"} {
		bad := prepared
		badPolicy := reference.policy
		switch kind {
		case "parent":
			bad.parent = request.RunID
		case "document":
			bad.document = uuid.New()
		case "binding":
			bad.source.binding = intent.Reference
		case "report":
			bad.qualification.report.ref.ArtifactID = uuid.Nil
		case "policy":
			badPolicy.body = "[]"
		case "scope":
			bad.source.session = uuid.New()
		}
		if _, err := reader.compileNativeTargetDetectorPlan(ctx, who, anchor, bad, badPolicy, 16<<20); err == nil {
			t.Fatal("target substitution accepted", kind)
		}
	}
	changed := plan
	changed.detector.shell.Config = append(json.RawMessage(nil), plan.detector.shell.Config...)
	changed.detector.shell.Config[0] = '['
	if err := verifyNativeTargetDetectorPlan(ctx, changed); err == nil {
		t.Fatal("mutated target plan accepted")
	}
	if _, err := reader.compileNativeTargetDetectorPlan(ctx, who, anchor, prepared, reference.policy, 1); err == nil {
		t.Fatal("underreserved target accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := reader.compileNativeTargetDetectorPlan(canceled, who, anchor, prepared, reference.policy, 16<<20); err == nil || got.seal != "" {
		t.Fatal("canceled target compiler exposed work")
	}
	if got, err := reader.beginNativeTargetDetectorInTransaction(ctx, nil, who, detectorDraftPlan{}, plan, TransferColorProfile{sha: prepared.source.iccSHA}); err == nil || got.RunID != uuid.Nil {
		t.Fatal("target detector begin without transaction/original authority accepted")
	}
	wrongActor := who
	wrongActor.UserID = uuid.New()
	if _, err := reader.compileNativeTargetDetectorPlan(ctx, wrongActor, anchor, prepared, reference.policy, 16<<20); err == nil {
		t.Fatal("foreign actor target compiler accepted")
	}
	wrongStage, err := compileNativeTargetAnchor(ctx, who, request, "target-detector", published, reference.anchor.snapshotRef, reference.anchor.snapshotBody, manifest, 0, "mask.person")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.compileNativeTargetDetectorPlan(ctx, who, wrongStage, prepared, reference.policy, 16<<20); err == nil {
		t.Fatal("person stage used for target detector")
	}

}
