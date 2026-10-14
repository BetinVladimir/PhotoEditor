package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func assertPersonDraftPlan(t *testing.T, catalog *detectorCatalogFixture, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, body []byte, params v2input.Ref, proof sourcePreparationProof) {
	t.Helper()
	snapshotID := uuid.New()
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: binding, ReferenceMask: v2input.Ref{ArtifactID: uuid.New(), SHA256: params.SHA256}, TargetSnapshotID: snapshotID, Strength: .5, PreservePersons: true}
	intentBody, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	request, err := CompileRequest(tenant, who.UserID, "person-plan", intentBody)
	if err != nil {
		t.Fatal(err)
	}
	_, childWorkflow, err := personPreparationIDs(request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	childCatalog := *catalog
	childCatalog.calls = map[uuid.UUID]int{}
	child, err := prepareCatalogPersonTask(context.Background(), &childCatalog, who, tenant, childWorkflow, 0, binding, body, params, strings.Repeat("a", 64), 512, v2input.SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	childDraft, err := child.frozen.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	childTaskSHA, err := canonical.Hash(childDraft)
	if err != nil {
		t.Fatal(err)
	}
	project := uuid.New()
	reader := SQLTransferSourceReader{ProjectID: project, Bucket: "bucket"}
	policyBody, err := canonical.Marshal([]qualificationPolicyRow{{ID: uuid.New(), Operation: "mask.person", Version: "1", Model: strings.Repeat("a", 64), Runtime: params.SHA256, Device: params.SHA256, Dataset: params.SHA256, Backend: "cuda", Issuer: uuid.New(), Checks: []string{"quality"}, Fixtures: []string{params.SHA256}, Evidence: []string{params.SHA256}, License: true, Enabled: true, Admission: true}})
	if err != nil {
		t.Fatal(err)
	}
	policy := qualificationPolicySnapshot{tenant: tenant, project: project, actor: who.UserID, session: who.SessionID, body: string(policyBody)}
	selected, err := selectQualificationPolicies(policy, []string{"mask.person"})
	if err != nil {
		t.Fatal(err)
	}
	expectedBody, err := canonical.Marshal(selected["mask.person"])
	if err != nil {
		t.Fatal(err)
	}
	full := sourcePersonPreparation{costs: v2input.SemanticQualifiedCosts{VRAMBytes: 1 << 30}, parent: request.RunID, document: uuid.NewSHA1(request.RunID, []byte("semantic.mask.person.params:reference")), catalog: child, source: sourcePreparationProof{tenant: tenant, project: project, actor: who.UserID, session: who.SessionID, bucket: "bucket", binding: binding, iccSHA: strings.Repeat("c", 64), unitRGB: true, pins: string([]byte(proof.pins))}, qualification: personQualificationProof{tenant: tenant, project: project, actor: who.UserID, session: who.SessionID, bucket: "bucket", taskSHA: childTaskSHA, expected: string(expectedBody), report: personEvidenceObject{ref: params, contract: "QualificationReport", backend: "cuda", object: objectstore.Object{Key: "report", VersionID: "v1", SHA256: params.SHA256, Bytes: 512}}}}
	snapshot, snapshotBody, err := v2input.CompilePinnedSessionInputSnapshot(snapshotID, []v2input.SessionSnapshotItem{{Ordinal: 0, Binding: binding, WorkingImage: binding.Source, Recipe: params}})
	if err != nil {
		t.Fatal(err)
	}
	snapshotSHA, err := canonical.Hash(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	intentSHA, err := canonical.Hash(intent)
	if err != nil {
		t.Fatal(err)
	}
	published := PublishedIntent{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: intentSHA}, Inventory: CatalogInventory{Scope: CatalogScope{ProjectID: project, ItemCount: 1}, Targets: []v2input.SessionVersionBinding{binding}}}
	plan, err := reader.compilePersonDraftPlan(context.Background(), who, request, "person-plan", published, v2input.Ref{ArtifactID: uuid.New(), SHA256: snapshotSHA}, snapshotBody, full, policy, 16<<20)
	if err != nil {
		t.Fatal("full person childDraft", err)
	}
 var newConfig map[string]json.RawMessage
 if json.Unmarshal(plan.shell.Config,&newConfig)!=nil||string(newConfig["queue_class"])!=`"batch"`{t.Fatal("new person queue missing")}
 oldPrepared:=full
 oldPrepared.legacyQueueOmitted=true
 historical,err:=reader.compilePersonDraftPlan(context.Background(),who,request,"person-plan",published,plan.anchor.snapshotRef,snapshotBody,oldPrepared,policy,16<<20)
 if err!=nil{t.Fatal(err)}
 var oldConfig map[string]json.RawMessage
 if json.Unmarshal(historical.shell.Config,&oldConfig)!=nil{t.Fatal("invalid historical person")}
 if _,present:=oldConfig["queue_class"];present{t.Fatal("historical queue omission changed")}
 replay,err:=reader.compilePersonDraftPlan(context.Background(),who,historical.request,historical.requestKey,historical.published,historical.anchor.snapshotRef,historical.anchor.snapshotBody,historical.prepared,historical.policy,historical.shell.Bytes)
 if err!=nil||replay.seal!=historical.seal||string(replay.shell.Config)!=string(historical.shell.Config){t.Fatal("historical person replay changed",err)}
	assertNativeTargetPersonPlan(t, reader, &childCatalog, who, plan, body, params)
	if plan.shell.Run == request.RunID || len(plan.shell.Tasks) != 1 || plan.shell.Tasks[0].ID != childDraft.ID {
		t.Fatal("child shell lost identity")
	}
	saved, err := decodeNativeInputSavedStage(plan.shell.Config, plan.shell.ConfigSHA, childDraft.ID, v2input.Ref{ArtifactID: childDraft.ParamsArtifactID, SHA256: childDraft.ParamsSHA}, childDraft.InputDigest, childDraft.ExecutionProfile, project, who, request, published.Ref, plan.anchor.snapshotRef, "mask.person")
	if err != nil || saved.recovery.RequestKey != "person-plan" || saved.recovery.Costs != full.costs || saved.recovery.Report == nil || *saved.recovery.Report != full.qualification.report.ref || saved.run != plan.shell.Run {
		t.Fatal("person shell lost original recovery inputs", err)
	}
	transport, packet, err := publishPersonDraftTransport(context.Background(), plan, &proposalMemoryTransport{})
	if err != nil || packet.Count != transport.Graph.Built.Count {
		t.Fatal("childDraft transport", err)
	}
	assertNativeRegistrationBinding(t, plan.prepared.catalog.frozen, transport.Graph.Built, "mask.person", func(ctx context.Context, built v2input.Built) (json.RawMessage, error) {
		return marshalPersonRegistrationBinding(ctx, plan, built)
	})
	assertNativeAdmissionEvidence(t, reader, who, request, plan.shell, plan.prepared.source, "mask.person", func(ctx context.Context) (json.RawMessage, error) {
		return reader.marshalPersonAdmissionEvidence(ctx, who, plan, transport.Graph.Built)
	})
	for _, kind := range []string{"source_tenant", "source_project", "source_actor", "source_session", "source_bucket", "icc", "unit_rgb"} {
		changed := plan
		mutateNativeSourceScope(kind, &changed.prepared.source)
		changed.seal, err = personPlanDigest(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := reader.marshalPersonAdmissionEvidence(context.Background(), who, changed, transport.Graph.Built); err == nil {
			t.Fatal("resealed invalid authority accepted", kind)
		}
	}
	assertNativeSourceLock(t, reader, who, request.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		return reader.lockPersonAdmissionSourceInTransaction(ctx, tx, who, plan, transport.Graph.Built)
	})
	assertNativeResume(t, transport.Graph.ID, transport.Graph.Built, v2input.JobTicket{RunID: plan.shell.Run, WorkflowID: plan.shell.WorkflowID, State: "queued"}, func(ctx context.Context, tx pgx.Tx) (v2input.JobTicket, error) {
		return reader.recoverAndSealPersonInTransaction(ctx, tx, who, plan)
	})
	assertFrozenNativeRecovery(t, transport.Graph.ID, transport.Graph.Built, func(ctx context.Context, tx pgx.Tx) (v2input.Built, error) {
		return recoverPersonTransportInTransaction(ctx, tx, who, plan)
	})
	assertNativeSeal(t, request.TenantID, plan.shell.Run, plan.shell.WorkflowID, func(ctx context.Context, tx pgx.Tx) (v2input.JobTicket, error) {
		return reader.sealPersonPreparationInTransaction(ctx, tx, who, plan, transport.Graph.Built)
	})
	registrationRequest := request
	registrationRequest.RunID = plan.shell.Run
	for _, expectedState := range []string{"missing", "replayed", "conflict"} {
		tx := &manualLockTx{role: expectedState}
		state, err := personPreparationRegistrationState(context.Background(), tx, registrationRequest, childDraft, transport.Graph)
		if err != nil || state != expectedState || len(tx.calls) != 1 || len(tx.args[0]) != 13 {
			t.Fatal("registration gate packet", state, err)
		}
		args := tx.args[0]
		if args[0] != tenant || args[1] != plan.shell.Run || args[2] != childDraft.ID || args[3] != childDraft.ParamsArtifactID || args[4] != childDraft.ParamsSHA || args[5] != childDraft.InputDigest || string(args[6].(json.RawMessage)) != string(childDraft.ExecutionProfile) ||
			string(args[7].(json.RawMessage)) != string(packet.Rows) || string(args[8].(json.RawMessage)) != string(packet.Manifest) || string(args[9].(json.RawMessage)) != string(packet.Chunks) || string(args[10].(json.RawMessage)) != string(packet.Direct) || args[11] != packet.Count || args[12] != packet.EntriesSHA {
			t.Fatal("registration changed immutable transport or parent scope")
		}
	}
	for _, kind := range []string{"source_session", "qualification_session", "icc", "unit_rgb", "report_backend", "source_binding"} {
		changed := plan
		switch kind {
		case "source_session":
			changed.prepared.source.session = uuid.New()
		case "qualification_session":
			changed.prepared.qualification.session = uuid.New()
		case "icc":
			changed.prepared.source.iccSHA = strings.Repeat("f", 64)
		case "unit_rgb":
			changed.prepared.source.unitRGB = false
		case "report_backend":
			changed.prepared.qualification.report.backend = "cpu"
		case "source_binding":
			changed.prepared.source.binding.AssetVersionID = uuid.New()
		}
		if _, _, err := publishPersonDraftTransport(context.Background(), changed, &proposalMemoryTransport{}); err == nil {
			t.Fatal("mutated scope reached publication", kind)
		}
	}
}
