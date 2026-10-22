package delivery

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestWatermarkCreateDocumentsRequireExactPins(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	scope, config, evidence := watermarkPinFixture(t)
	scope.Tenant = intent.tenant
	scope.Project = intent.request.ProjectID
	scope.Actor = intent.actor
	scope.Parent = intent.identity.Run
	scope.Preset = catalog.Presets[0].ID
	catalog.Presets[0].ConfigJCS = config
	catalog.Presets[0].SHA256 = scope.PresetSHA
	pin, err := CompileWatermarkSourcePin(scope, config, evidence)
	if err != nil {
		t.Fatal(err)
	}
	pins := []PreparedWatermarkPin{{Preset: scope.Preset, PresetSHA: scope.PresetSHA, Pin: pin}}
	docs, err := intent.CompileWatermarkDocuments(context.Background(), session, catalog, items, pins)
	if err != nil {
		t.Fatal(err)
	}
	if docs.Plan.TaskID() != intent.identity.PrepareTask || docs.Params.Ref.ArtifactID != intent.identity.Params || docs.Frozen.Ref.ArtifactID != intent.identity.FrozenInput {
		t.Fatal("document identity changed")
	}
	again, err := intent.CompileWatermarkDocuments(context.Background(), uuid.New(), catalog, items, pins)
	if err != nil || docs.Params.Ref != again.Params.Ref || !bytes.Equal(docs.Frozen.Body, again.Frozen.Body) {
		t.Fatal("session changed immutable documents", err)
	}
	if _, err := intent.CompileDocuments(context.Background(), session, catalog, items); err == nil {
		t.Fatal("ordinary path accepted watermark")
	}
	for _, bad := range [][]PreparedWatermarkPin{nil, {pins[0], pins[0]}, {{Preset: uuid.New(), PresetSHA: scope.PresetSHA, Pin: pin}}} {
		if _, err := intent.CompileWatermarkDocuments(context.Background(), session, catalog, items, bad); err == nil {
			t.Fatal("bad pin inventory")
		}
	}
	pendingRaw, err := intent.EncodeWatermarkPending(context.Background(), session, catalog, items, pins)
	if err != nil {
		t.Fatal(err)
	}
	selectionRaw, err := intent.EncodeWatermarkSelectionPending(context.Background(), session, catalog, items)
	if err != nil || bytes.Equal(selectionRaw, pendingRaw) { t.Fatal("selection lost distinct stage", err) }
	callerBytes := append([]byte(nil), selectionRaw...)
	ownedSelection, ownErr := intent.ownWatermarkSelectionPending(context.Background(), session, callerBytes)
	if ownErr != nil || !bytes.Equal(ownedSelection, selectionRaw) {
		t.Fatal("selection ownership changed payload", ownErr)
	}
	callerBytes[0] = '!'
	if !bytes.Equal(ownedSelection, selectionRaw) {
		t.Fatal("caller mutation changed owned selection")
	}
	if _, ownErr = intent.DecodeWatermarkPending(context.Background(), session, ownedSelection, pins); ownErr == nil {
		t.Fatal("selection acquired executable authority without promotion")
	}
	if _, ownErr = intent.ownWatermarkSelectionPending(context.Background(), session, callerBytes); ownErr == nil {
		t.Fatal("malformed selection acquired ownership")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if value, ownErr := intent.ownWatermarkSelectionPending(cancelled, session, pendingRaw); ownErr == nil || value != nil {
		t.Fatal("cancelled selection acquired ownership", ownErr)
	}

	if _, _, err := (Service{}).PinWatermarkSelectionPending(context.Background(), auth.Identity{UserID: intent.actor, SessionID: session}, intent.tenant, intent.Request(), intent.key, catalog, items); err == nil {
		t.Fatal("missing database accepted")
	}
	selectionPending, err := intent.DecodeWatermarkSelectionPending(context.Background(), session, selectionRaw)
	if err != nil {
		t.Fatal(err)
	}
	shellTX := &proofTx{accepted: true}
	shellWho := auth.Identity{UserID: intent.actor, SessionID: session}
	if err := intent.BeginWatermarkSelectionRun(context.Background(), shellTX, shellWho, intent.tenant, selectionPending, "photos"); err != nil || shellTX.calls != 1 || len(shellTX.args) != 7 || shellTX.args[0] != intent.tenant || shellTX.args[4] != intent.RequestSHA() {
		t.Fatal("selection shell ABI", err)
	}
	shellTX.accepted = false
	if err := intent.BeginWatermarkSelectionRun(context.Background(), shellTX, shellWho, intent.tenant, selectionPending, "photos"); err == nil {
		t.Fatal("shell refusal ignored")
	}
	beforeShell := shellTX.calls
	if err := intent.BeginWatermarkSelectionRun(context.Background(), shellTX, shellWho, uuid.New(), selectionPending, "photos"); err == nil || shellTX.calls != beforeShell {
		t.Fatal("foreign shell queried")
	}
	if _, err := (Service{}).StageWatermarkSelectionSources(context.Background(), shellWho, intent.tenant, intent.Request(), intent.key, selectionRaw, "photos", nil); err == nil {
		t.Fatal("missing stage dependencies")
	}
	projected, err := intent.projectCreateDocuments(context.Background(), session, catalog, items)
	if err != nil || projected.Plan.TaskID() != uuid.Nil {
		t.Fatal("selection granted executable plan", err)
	}
	if _, err := intent.DecodeWatermarkSelectionPending(context.Background(), uuid.New(), selectionRaw); err != nil {
		t.Fatal("selection retry", err)
	}
	pending, err := intent.DecodeWatermarkPending(context.Background(), uuid.New(), pendingRaw, pins)
	if err != nil || pending.Actor != scope.Actor || pending.Tenant != scope.Tenant {
		t.Fatal("pending restore", err)
	}
	if _, err := intent.DecodePending(context.Background(), session, pendingRaw); err == nil {
		t.Fatal("ordinary pending accepted watermark")
	}
	if _, err := intent.DecodeWatermarkPending(context.Background(), session, pendingRaw, nil); err == nil {
		t.Fatal("missing pin recovery")
	}
	tx := &proofTx{accepted: true}
	who := auth.Identity{UserID: scope.Actor, SessionID: session}
	if err := intent.AdmitWatermarkPreparedPackage(context.Background(), tx, who, scope.Tenant, pendingRaw, pins); err != nil || tx.calls != 2 {
		t.Fatal("proof/package admission", err, tx.calls)
	}
	tx.accepted = false
	before := tx.calls
	if err := intent.AdmitWatermarkPreparedPackage(context.Background(), tx, who, scope.Tenant, pendingRaw, pins); err == nil || tx.calls != before+1 {
		t.Fatal("proof refusal continued package admission")
	}
	pending.Documents[0].Body = "{}"
	tampered, err := canonical.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	before = tx.calls
	if err := intent.AdmitWatermarkPreparedPackage(context.Background(), tx, who, scope.Tenant, tampered, pins); err == nil || tx.calls != before {
		t.Fatal("tampered pending queried SQL")
	}
	resolverCalls := 0
	resolver := createRecipeResolverFunc(func(_ context.Context, actor auth.Identity, tid, project uuid.UUID, item v2input.SessionSnapshotItem) ([]v2input.Entry, error) {
		resolverCalls++
		if actor.UserID != intent.actor || tid != intent.tenant || project != intent.request.ProjectID {
			t.Fatal("closure scope")
		}
		return []v2input.Entry{{Ref: item.WorkingImage, Contract: "WorkingImage"}, {Ref: item.Recipe, Contract: "FrozenRenderRecipe"}}, nil
	})
	frozenPrepare, err := intent.ResolveWatermarkPrepareTask(context.Background(), who, pendingRaw, resolver, PrepareResourceLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 8 << 20}, pins)
	if err != nil || resolverCalls != len(items) {
		t.Fatal("watermark closure", err)
	}
	prepareDraft, err := frozenPrepare.DraftTask()
	if err != nil || prepareDraft.ID != intent.identity.PrepareTask {
		t.Fatal("prepare identity", err)
	}
	callsBefore := resolverCalls
	if _, err := intent.ResolveWatermarkPrepareTask(context.Background(), who, pendingRaw, resolver, PrepareResourceLimits{}, nil); err == nil || resolverCalls != callsBefore {
		t.Fatal("missing pins reached resolver")
	}
	publisher := &createPublisherStub{}
	if err := intent.PublishDocuments(context.Background(), docs, publisher); err != nil || len(publisher.calls) != 5 {
		t.Fatal("watermark publication", err)
	}
	materializeTX := &proofTx{accepted: true}
	if err := intent.MaterializePrepare(context.Background(), materializeTX, who, intent.tenant, frozenPrepare); err != nil || materializeTX.calls != 1 {
		t.Fatal("watermark materialization", err)
	}
	if _, err := (Service{}).StageWatermarkCreatePrepare(context.Background(), who, intent.tenant, intent.Request(), intent.key, pendingRaw, "photos", nil, PrepareResourceLimits{}); err == nil {
		t.Fatal("missing stage dependencies accepted")
	}
	config[0] = '!'
	if docs.Presets[0].Body[0] != '{' {
		t.Fatal("borrowed config")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := intent.CompileWatermarkDocuments(ctx, session, catalog, items, pins); err != context.Canceled {
		t.Fatal("cancel", err)
	}
}
