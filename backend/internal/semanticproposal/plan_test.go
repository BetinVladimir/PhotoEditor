package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestWorkflowPlanBindsPublishedIntentCatalogAndTasks(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{uuid.New(), strings.Repeat("a", 64)} }
	source := v2input.SessionVersionBinding{uuid.New(), ref(), strings.Repeat("b", 64)}
	target := v2input.SessionVersionBinding{uuid.New(), ref(), strings.Repeat("c", 64)}
	run := uuid.New()
	workflow, _ := v2input.WorkflowID(run)
	snapshotID := uuid.New()
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: source, ReferenceMask: ref(), TargetSnapshotID: snapshotID, Strength: .5, PreservePersons: true}
	intentBody, _ := canonical.Marshal(intent)
	intentRef := ref()
	intentRef.SHA256, _ = canonical.Hash(intent)
	request, err := CompileRequest(uuid.New(), uuid.New(), "workflow-plan", intentBody)
	if err != nil {
		t.Fatal(err)
	}
	run, workflow = request.RunID, request.WorkflowID
	snapshot, snapshotBody, err := v2input.CompilePinnedSessionInputSnapshot(snapshotID, []v2input.SessionSnapshotItem{{Ordinal: 0, Binding: target, WorkingImage: target.Source, Recipe: ref()}})
	if err != nil {
		t.Fatal(err)
	}
	snapshotRef := ref()
	snapshotRef.SHA256, _ = canonical.Hash(snapshot)
	qualification := v2input.SemanticQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: strings.Repeat("b", 64), Report: ref(), Calibration: ref(), ModelLockSHA: strings.Repeat("c", 64), RuntimeLockSHA: strings.Repeat("d", 64), DeviceProfileSHA: strings.Repeat("e", 64), DatasetSHA: strings.Repeat("f", 64), IssuerID: uuid.New(), Backend: "cuda"}
	params := v2input.SemanticTransferParams{1, intentRef, source, target, qualification.ModelLockSHA, qualification.Calibration}
	paramsRef := ref()
	paramsRef.SHA256, _ = canonical.Hash(params)
	dependencies := v2input.SemanticTransferDependencies{ReferenceWidth: 24, ReferenceHeight: 20, TargetWidth: 24, TargetHeight: 20, ReferenceGamma: true, TargetGamma: true, Reference: v2input.CaptureImageInputs{Pixels: ref()}, Target: v2input.CaptureImageInputs{Pixels: ref()}, SelectedPixels: ref(), SourceProtection: ref(), TargetProtection: ref(), SourceProtectionPixels: ref(), TargetProtectionPixels: ref()}
	profile, err := v2input.CompileSemanticTransferProfile(24, 20, 24, 20, true, true, v2input.SemanticQualifiedCosts{}, 1<<40, 1<<40, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	task, err := v2input.CompileSemanticTransferTask(workflow, 0, params, intent, paramsRef, dependencies, profile)
	if err != nil {
		t.Fatal(err)
	}

	project, session := uuid.New(), uuid.New()
	published := PublishedIntent{Ref: intentRef, Inventory: CatalogInventory{Scope: CatalogScope{ProjectID: project, ItemCount: 1}, Targets: []v2input.SessionVersionBinding{target}}}
	plan, err := CompileWorkflowPlan(request, session, published, snapshotRef, snapshotBody, []v2input.FrozenSemanticTask{task}, qualification)
	if err != nil || plan.RunID() != run || plan.WorkflowID() != workflow || plan.ProjectID() != project || plan.SnapshotID() != snapshotID || plan.IntentRef() != intentRef || plan.RequestSHA() != request.RequestSHA {
		t.Fatal("workflow plan", err)
	}

	// Exercise historical serialization using a fully compiled task graph,
	// then independently compile again as the scope validators do.
	historical := plan
	if err := omitNativeTransferQueue(&historical); err != nil { t.Fatal(err) }
	recompiled, err := CompileWorkflowPlan(request, session, published, snapshotRef, snapshotBody, []v2input.FrozenSemanticTask{task}, qualification)
	if err != nil { t.Fatal(err) }
	if err := omitNativeTransferQueue(&recompiled); err != nil { t.Fatal(err) }
	if historical.configSHA != recompiled.configSHA || string(historical.config) != string(recompiled.config) || historical.tasks[0].InputDigest != recompiled.tasks[0].InputDigest || historical.tasks[0].ID != recompiled.tasks[0].ID { t.Fatal("historical graph recompile changed") }
	if historical.configSHA == plan.configSHA { t.Fatal("queue missing from config hash") }
	var originalConfig, historicalConfig map[string]json.RawMessage
	if json.Unmarshal(plan.config, &originalConfig) != nil || json.Unmarshal(historical.config, &historicalConfig) != nil { t.Fatal("graph config") }
	if string(originalConfig["queue_class"]) != `"batch"` { t.Fatal("fresh graph queue changed") }
	if _, present := historicalConfig["queue_class"]; present { t.Fatal("historical graph queue fabricated") }

	transport := &proposalMemoryTransport{}
	prepared, err := PublishWorkflowTasks(context.Background(), plan, transport)
	if err != nil || len(prepared) != 1 || prepared[0].Graph.ID != plan.tasks[0].ID || prepared[0].InputDigest != plan.tasks[0].InputDigest || prepared[0].Graph.Built.Count < 1 || transport.calls != 2 {
		t.Fatal("workflow transport", err)
	}
	graph, err := BindWorkflowTransport(context.Background(), plan, []v2input.Built{prepared[0].Graph.Built})
	if err != nil || len(graph) != 1 || graph[0].ID != plan.tasks[0].ID {
		t.Fatal("transport graph binding", err)
	}
	badBuilt := prepared[0].Graph.Built
	badBuilt.SHA256 = strings.Repeat("0", 64)
	if _, err := BindWorkflowTransport(context.Background(), plan, []v2input.Built{badBuilt}); err == nil {
		t.Fatal("foreign closure digest bound")
	}
	badBuilt = prepared[0].Graph.Built
	badBuilt.Count++
	if _, err := BindWorkflowTransport(context.Background(), plan, []v2input.Built{badBuilt}); err == nil {
		t.Fatal("foreign closure count bound")
	}
	replayed, err := PublishWorkflowTasks(context.Background(), plan, transport)
	if err != nil || replayed[0].Graph.Built.Manifest != prepared[0].Graph.Built.Manifest {
		t.Fatal("transport replay", err)
	}
	failing := &proposalMemoryTransport{failAt: 2}
	if partial, err := PublishWorkflowTasks(context.Background(), plan, failing); err == nil || partial != nil {
		t.Fatal("partial transport graph returned", err)
	}
	corrupt := plan
	corrupt.tasks = append([]v2input.DraftTask(nil), plan.tasks...)
	corrupt.tasks[0].InputDigest = strings.Repeat("0", 64)
	untouched := &proposalMemoryTransport{}
	if _, err := PublishWorkflowTasks(context.Background(), corrupt, untouched); err == nil || untouched.calls != 0 {
		t.Fatal("corrupt batch published", err)
	}

	batchInputs := []TransferTaskInputs{{Dependencies: dependencies, Profile: profile}}
	batchPlan, err := CompileTransferWorkflowPlan(request, session, published, snapshotRef, snapshotBody, []v2input.Ref{paramsRef}, batchInputs, qualification)
	if err != nil || batchPlan.ConfigSHA() != plan.ConfigSHA() {
		t.Fatal("transfer batch plan", err)
	}
	if _, err := CompileTransferWorkflowPlan(request, session, published, snapshotRef, snapshotBody, nil, batchInputs, qualification); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("missing parameter publication", err)
	}

	for _, documents := range [][]v2input.SemanticPinnedDocuments{nil, {{Reference: []byte(`{}`), Target: []byte(`{}`)}}} {
		if _, err := CompileTransferWorkflowPlanFromDocuments(request, session, published, snapshotRef, snapshotBody, []v2input.Ref{paramsRef}, batchInputs, qualification, documents); err == nil {
			t.Fatal("document plan accepted missing or unpinned sources")
		}
	}
	wrongParams := paramsRef
	wrongParams.SHA256 = strings.Repeat("0", 64)
	if _, err := CompileTransferWorkflowPlan(request, session, published, snapshotRef, snapshotBody, []v2input.Ref{wrongParams}, batchInputs, qualification); err == nil {
		t.Fatal("wrong published parameter hash")
	}
	shell, err := plan.DraftShell(1 << 40)
	if err != nil || shell.Workflow != "semantic" || shell.Tenant != request.TenantID || shell.Actor != request.ActorID || shell.PolicySHA != qualification.PolicySHA || shell.ConfigSHA != plan.ConfigSHA() {
		t.Fatal("draft shell", err)
	}
	for _, size := range []int64{0, 1, 9007199254740992} {
		if _, err := plan.DraftShell(size); err == nil {
			t.Fatal("invalid reservation", size)
		}
	}
	shell.Config[0] = '!'
	shell.Tasks[0].ExecutionProfile[0] = '!'
	if plan.ConfigJSON()[0] != '{' || plan.DraftTasks()[0].ExecutionProfile[0] != '{' {
		t.Fatal("mutable shell aliases plan")
	}
	config := plan.ConfigJSON()
	config[0] = '!'
	if plan.ConfigJSON()[0] != '{' {
		t.Fatal("borrowed config")
	}
	drafts := plan.DraftTasks()
	if len(drafts) != 1 {
		t.Fatal("task count")
	}
	drafts[0].ExecutionProfile[0] = '!'
	if plan.DraftTasks()[0].ExecutionProfile[0] != '{' {
		t.Fatal("borrowed task profile")
	}
	who := auth.Identity{UserID: request.ActorID, SessionID: session}

	if _, err := PublishTransferParams(context.Background(), v2draft.Publisher{}, who, request, "other-key", published, qualification.ModelLockSHA, qualification.Calibration); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("wrong transfer key", err)
	}
	duplicate := published
	duplicate.Inventory.Targets = []v2input.SessionVersionBinding{target, target}
	duplicate.Inventory.Scope.ItemCount = 2
	if _, err := PublishTransferParams(context.Background(), v2draft.Publisher{}, who, request, "workflow-plan", duplicate, qualification.ModelLockSHA, qualification.Calibration); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("duplicate transfer target", err)
	}
	if _, err := PublishTransferParams(context.Background(), v2draft.Publisher{}, who, request, "workflow-plan", published, "invalid", qualification.Calibration); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("invalid transfer model", err)
	}
	// Identity failures must reject before touching a transaction.
	inert := &inertAdmissionTx{}
	if _, err := BeginDraftInTransaction(context.Background(), inert, who, request, "other-key", published, plan, 1<<40); err == nil {
		t.Fatal("wrong key accepted")
	}
	otherSession := who
	otherSession.SessionID = uuid.New()
	if _, err := BeginDraftInTransaction(context.Background(), inert, otherSession, request, "workflow-plan", published, plan, 1<<40); err == nil {
		t.Fatal("different frozen session accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BeginDraftInTransaction(cancelled, inert, who, request, "workflow-plan", published, plan, 1<<40); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled admission reached SQL", err)
	}
	for _, size := range []int64{0, 1, 9007199254740992} {
		if _, err := BeginDraftInTransaction(context.Background(), inert, who, request, "workflow-plan", published, plan, size); err == nil {
			t.Fatal("invalid admission reservation", size)
		}
	}
	targetCopy := plan.Inventory()
	targetCopy.Targets[0].CanvasHash = strings.Repeat("0", 64)
	if plan.Inventory().Targets[0] != target {
		t.Fatal("borrowed inventory")
	}
	published.Inventory.Targets[0].CanvasHash = strings.Repeat("0", 64)
	if _, err := BeginDraftInTransaction(context.Background(), inert, who, request, "workflow-plan", published, plan, 1<<40); err == nil {
		t.Fatal("admission accepted changed published inventory")
	}
	if _, err := CompileWorkflowPlan(request, session, published, snapshotRef, snapshotBody, []v2input.FrozenSemanticTask{task}, qualification); err == nil {
		t.Fatal("catalog/snapshot drift accepted")
	}
	published.Inventory.Targets[0] = target
	changed := request
	changed.RequestSHA = strings.Repeat("0", 64)
	if _, err := CompileWorkflowPlan(changed, session, published, snapshotRef, snapshotBody, []v2input.FrozenSemanticTask{task}, qualification); err == nil {
		t.Fatal("request fence drift accepted")
	}
}

type inertAdmissionTx struct{ pgx.Tx }

// Stable transport identities exercise replay and the manifest failure boundary.
type proposalMemoryTransport struct{ calls, failAt int }

func (p *proposalMemoryTransport) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	p.calls++
	if p.failAt == p.calls {
		return v2input.Ref{}, errors.New("transport failure")
	}
	sha, err := canonical.Hash(json.RawMessage(body))
	if err != nil {
		return v2input.Ref{}, err
	}
	return v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte(contract+sha)), SHA256: sha}, nil
}
