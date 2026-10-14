package v2optical

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestPerspectiveOwnedOriginalQualificationAndProfile(t *testing.T) {
	read := func(path string) []byte {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var fixture struct {
		Pin    PerspectiveQualificationPin
		Locks  v2qualification.DeploymentLocks
		Report string
	}
	if err := json.Unmarshal(read("testdata/negative-qualification.json"), &fixture); err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(fixture.Report), &report); err != nil {
		t.Fatal(err)
	}
	report["operation"] = "optical.perspective"
	reportRaw, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Pin.Report.SHA256, err = canonical.Hash(report)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Locks.Operation = "optical.perspective"
	var structural struct {
		Fixtures map[string]json.RawMessage `json:"fixtures"`
	}
	if err := json.Unmarshal(read("../../../docs/implementation-v2/contracts/structural-fixtures.json"), &structural); err != nil {
		t.Fatal(err)
	}
	var calibration map[string]any
	if err := json.Unmarshal(structural.Fixtures["Calibration"], &calibration); err != nil {
		t.Fatal(err)
	}
	calibration["qualified"] = true
	calibration["model_lock_sha"] = fixture.Pin.ModelLockSHA
	calibration["dataset_sha"] = fixture.Pin.DatasetSHA
	calibrationRaw, err := canonical.Marshal(calibration)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Pin.Calibration.SHA256, err = canonical.Hash(calibration)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Locks.CalibrationSHAs = []string{fixture.Pin.Calibration.SHA256}
	fixture.Pin.PolicySHA, err = PerspectivePolicySHA(fixture.Pin.PolicyID, fixture.Locks)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(read("testdata/negative-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	sourceRaw, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	params := perspectiveIntent()
	params.Base.Source.SHA256, err = canonical.Hash(image)
	if err != nil {
		t.Fatal(err)
	}
	params.Base.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyPerspectiveQualificationEvidence(fixture.Pin, fixture.Locks, reportRaw, calibrationRaw, params, image.Color.ICCSHA256)
	if err != nil {
		t.Fatal(err)
	}
	required, err := PerspectiveResources(image.Canvas.Width, image.Canvas.Height, image.Canvas.Width, image.Canvas.Height)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompilePerspectiveManualExecutionProfile(params, sourceRaw, verified, 200000000, required); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ok", "corrupt", "oversize", "changed"} {
		store := &perspectiveEvidenceIO{negativeEvidenceIO: negativeEvidenceIO{
			r: append([]byte(nil), reportRaw...), c: append([]byte(nil), calibrationRaw...), mode: mode,
			report: objectstore.Object{Key: "report", VersionID: "v1", SHA256: fixture.Pin.Report.SHA256, Bytes: int64(len(reportRaw)), ContentType: "application/json"},
			cal:    objectstore.Object{Key: "cal", VersionID: "v1", SHA256: fixture.Pin.Calibration.SHA256, Bytes: int64(len(calibrationRaw)), ContentType: "application/json"},
		}}
		located, loadErr := LoadPerspectiveQualificationEvidence(context.Background(), store, store,
			auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), fixture.Pin, fixture.Locks, params, image.Color.ICCSHA256)
		if mode == "ok" {
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			objects, err := located.Objects()
			if err != nil || len(objects) != 2 || objects[0].Contract != "Calibration" || objects[1].Contract != "Report" {
				t.Fatal(objects, err)
			}
			if store.catalogs < 6 {
				t.Fatal("missing live revalidation", store.catalogs)
			}
		} else if loadErr == nil {
			t.Fatal("object substitution accepted", mode)
		}
		if store.closes != store.reads {
			t.Fatal("stream leak", mode, store.reads, store.closes)
		}
	}
	plan, err := CompilePerspectiveTaskPlan(context.Background(), params.Base, mustPerspectiveParams(t, params), sourceRaw, verified, 200000000, required)
	if err != nil {
		t.Fatal(err)
	}

	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project, snapshot := uuid.New(), uuid.New(), uuid.New()
	draft, err := CompilePerspectiveDraft(context.Background(), who, tenant, project, snapshot, "perspective-draft-test", plan.Params, sourceRaw, verified, 200000000, required)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := draft.Snapshot()
	if err != nil || saved.ParamsSHA != plan.ParamsSHA || saved.ReservedBytes != plan.ReservedBytes || saved.Workflow != uuid.NewSHA1(saved.Run, []byte("workflow")) {
		t.Fatal("draft binding", err)
	}
	saved.Profile[0] = '!'
	owned, err := draft.Snapshot()
	if err != nil || owned.Profile[0] == '!' {
		t.Fatal("draft snapshot aliases", err)
	}
	repeated, err := CompilePerspectiveDraft(context.Background(), who, tenant, project, snapshot, "perspective-draft-test", plan.Params, sourceRaw, verified, 200000000, required)
	if err != nil || repeated.raw != draft.raw {
		t.Fatal("draft replay", err)
	}
	repository := perspectiveParamsRepository{db: perspectiveArrayDB{}, bucket: "photos", tenant: tenant, who: who, plan: draft}
	scope := artifactjson.Scope{Tenant: tenant, Run: saved.Run, Owner: saved.Task, Purpose: "task_params"}
	if _, err := repository.proof(scope, plan.ParamsSHA, int64(len(plan.Params))); err != nil {
		t.Fatal("publication proof", err)
	}
	foreign := scope
	foreign.Owner = uuid.New()
	if _, err := repository.proof(foreign, plan.ParamsSHA, int64(len(plan.Params))); err == nil {
		t.Fatal("foreign Params owner")
	}
	foreign = scope
	foreign.DonorPortrait = true
	if _, err := repository.proof(foreign, plan.ParamsSHA, int64(len(plan.Params))); err == nil {
		t.Fatal("foreign publication path")
	}
	if _, err := repository.proof(scope, plan.ParamsSHA, int64(len(plan.Params))+1); err == nil {
		t.Fatal("changed Params size")
	}

	locatedForDraft, err := BindPerspectiveQualificationObjects(verified, "photos",
		objectstore.Object{Key: "report", VersionID: "v1", SHA256: fixture.Pin.Report.SHA256, Bytes: int64(len(reportRaw)), ContentType: "application/json"},
		objectstore.Object{Key: "cal", VersionID: "v1", SHA256: fixture.Pin.Calibration.SHA256, Bytes: int64(len(calibrationRaw)), ContentType: "application/json"})
	if err != nil {
		t.Fatal(err)
	}
	prepared := PreparedPerspective{draft: draft, params: artifactjson.Ref{ID: saved.ParamsDocument, SHA256: saved.ParamsSHA}, qualification: locatedForDraft}
	descriptor, err := prepared.Descriptor(context.Background())
	if err != nil || len(descriptor) < 2 {
		t.Fatal("perspective descriptor", err)
	}
	descriptorAgain, err := prepared.Descriptor(context.Background())
	if err != nil || string(descriptor) != string(descriptorAgain) {
		t.Fatal("descriptor replay", err)
	}

	var transportDescriptor struct {
		Entries []v2input.Entry `json:"entries"`
	}
	if err := json.Unmarshal(descriptor, &transportDescriptor); err != nil {
		t.Fatal(err)
	}
	entriesHash, err := canonical.Hash(transportDescriptor.Entries)
	if err != nil {
		t.Fatal(err)
	}
	chunkHash, err := canonical.Hash(v2input.Chunk{SchemaVersion: 1, Entries: transportDescriptor.Entries})
	if err != nil {
		t.Fatal(err)
	}
	chunkRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: chunkHash}
	manifestHash, err := canonical.Hash(v2input.Manifest{SchemaVersion: 1, TaskID: saved.Task, EntryCount: len(transportDescriptor.Entries), EntriesSHA: entriesHash, Chunks: []v2input.Ref{chunkRef}})
	if err != nil {
		t.Fatal(err)
	}
	transportWire := map[string]any{"manifest": v2input.Ref{ArtifactID: uuid.New(), SHA256: manifestHash}, "chunks": []v2input.Ref{chunkRef}, "count": len(transportDescriptor.Entries), "sha256": entriesHash}
	for _, fault := range []string{"", "count", "entries_sha", "chunk_sha", "manifest_sha", "alias", "unknown", "missing"} {
		wire := map[string]any{}
		for key, value := range transportWire {
			wire[key] = value
		}
		switch fault {
		case "count":
			wire["count"] = 0
		case "entries_sha":
			wire["sha256"] = plan.ParamsSHA
		case "chunk_sha":
			wire["chunks"] = []v2input.Ref{{ArtifactID: chunkRef.ArtifactID, SHA256: plan.ParamsSHA}}
		case "manifest_sha":
			wire["manifest"] = v2input.Ref{ArtifactID: uuid.New(), SHA256: plan.ParamsSHA}
		case "alias":
			wire["manifest"] = v2input.Ref{ArtifactID: chunkRef.ArtifactID, SHA256: manifestHash}
		case "unknown":
			wire["extra"] = true
		case "missing":
			delete(wire, "count")
		}
		raw, err := canonical.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := prepared.RestorePublishedTransport(context.Background(), raw)
		if fault == "" {
			if err != nil || restored.ID != saved.Task || len(restored.Direct) != 4 {
				t.Fatal("restore", err)
			}
			cursor, err := restored.Open(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			entry, ok, err := cursor.Next(context.Background())
			if err != nil || !ok || entry != transportDescriptor.Entries[0] {
				t.Fatal("restored cursor", err)
			}
		} else if err == nil {
			t.Fatal("invalid transport restored", fault)
		}
	}

	receiptTransport, err := canonical.Marshal(transportWire)
	if err != nil {
		t.Fatal(err)
	}
	descriptorSHA, err := canonical.Hash(json.RawMessage(descriptor))
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"", "run", "session", "descriptor", "qualification", "transport"} {
		receipt := PerspectiveReceipt{SchemaVersion: 1, Run: saved.Run, Workflow: saved.Workflow, Task: saved.Task, State: "queued", Actor: who.UserID, Session: who.SessionID, Operation: "optical.perspective", DescriptorSHA: descriptorSHA, Qualification: fixture.Pin, Transport: append(json.RawMessage(nil), receiptTransport...)}
		switch fault {
		case "run":
			receipt.Run = uuid.New()
		case "session":
			receipt.Session = uuid.New()
		case "descriptor":
			receipt.DescriptorSHA = plan.ParamsSHA
		case "qualification":
			receipt.Qualification.Report.ArtifactID = uuid.New()
		case "transport":
			receipt.Transport = json.RawMessage(`{}`)
		}
		raw, err := canonical.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		_, err = parsePerspectiveReceipt(context.Background(), prepared, raw)
		if (err == nil) != (fault == "") {
			t.Fatal("receipt validation", fault, err)
		}
	}

	deploymentEntry := perspectiveDeploymentEntry{Tenant: tenant, Project: project, Pin: fixture.Pin, Locks: fixture.Locks, MaxPixels: 200000000, Capacity: required}
	for _, fault := range []string{"", "duplicate", "policy", "tenant", "capacity", "vram", "operation", "unknown"} {
		entry := deploymentEntry
		switch fault {
		case "policy":
			entry.Pin.PolicySHA = plan.ParamsSHA
		case "tenant":
			entry.Tenant = uuid.Nil
		case "capacity":
			entry.Capacity.RAMBytes = 0
		case "vram":
			entry.Capacity.VRAMBytes = 1
		case "operation":
			entry.Locks.Operation = "optical.lens"
		}
		entries := []perspectiveDeploymentEntry{entry}
		if fault == "duplicate" {
			entries = append(entries, entry)
		}
		wire := map[string]any{"schema_version": 1, "projects": entries}
		if fault == "unknown" {
			wire["extra"] = true
		}
		raw, err := canonical.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		deployment, err := DecodePerspectiveDeployment(raw)
		if fault == "" {
			if err != nil {
				t.Fatal("valid perspective deployment", err)
			}
			raw[0] = '!'
			if _, err := DecodePerspectiveDeployment([]byte(deployment.raw)); err != nil {
				t.Fatal("deployment buffer ownership", err)
			}
		} else if err == nil {
			t.Fatal("invalid perspective deployment", fault)
		}
	}

	stageRequest := PerspectivePrepareRequest{Tenant: tenant, Project: project, Snapshot: snapshot, Key: "perspective-draft-test", Params: append([]byte(nil), plan.Params...), Source: append([]byte(nil), sourceRaw...)}
	stageCoordinator := PerspectiveCoordinator{DB: noPerspectivePreparationDB{}, Reader: noPerspectivePreparationIO{}, Bucket: "photos", MaxPixels: 200000000, Capacity: required}
	for _, fault := range []string{"tenant", "snapshot", "params", "source", "key", "session", "cancel"} {
		request := stageRequest
		principal := who
		ctx, cancel := context.WithCancel(context.Background())
		switch fault {
		case "tenant":
			request.Tenant = uuid.New()
		case "snapshot":
			request.Snapshot = uuid.New()
		case "params":
			request.Params = []byte("{}")
		case "source":
			request.Source = []byte("{}")
		case "key":
			request.Key = "changed-perspective-key"
		case "session":
			principal.SessionID = uuid.New()
		case "cancel":
			cancel()
		}
		err := stageCoordinator.Stage(ctx, principal, request, prepared)
		if err == nil {
			t.Fatal("invalid stage reached IO", fault)
		}
		if _, err := stageCoordinator.ReadPublishedTransport(ctx, principal, request, prepared); err == nil {
			t.Fatal("invalid recovery reached IO", fault)
		}
		if _, err := stageCoordinator.RegisterInputs(ctx, principal, request, prepared); err == nil {
			t.Fatal("invalid registration reached IO", fault)
		}
		if err := stageCoordinator.Admit(ctx, principal, request, prepared); err == nil {
			t.Fatal("invalid admission reached IO", fault)
		}

		cancel()
	}

	transportBody := `{"schema_version":1}`
	transportSHA, err := canonical.Hash(json.RawMessage(transportBody))
	if err != nil {
		t.Fatal(err)
	}
	transportRepo := perspectiveTransportRepository{coordinator: PerspectiveCoordinator{DB: noPerspectivePreparationDB{}, Bucket: "photos"}, prepared: prepared, body: transportBody}
	transportScope := artifactjson.Scope{Tenant: tenant, Run: saved.Run, Owner: saved.Task, Purpose: "v2_input_chunk_0"}
	if _, err := transportRepo.proof(transportScope, transportSHA, int64(len(transportBody))); err != nil {
		t.Fatal("transport scope", err)
	}
	for _, fault := range []string{"owner", "purpose", "lease", "size", "sha", "cancel"} {
		scope := transportScope
		digest := transportSHA
		size := int64(len(transportBody))
		ctx, cancel := context.WithCancel(context.Background())
		switch fault {
		case "owner":
			scope.Owner = uuid.New()
		case "purpose":
			scope.Purpose = "task_params"
		case "lease":
			scope.AssemblyLease = uuid.New()
		case "size":
			size++
		case "sha":
			digest = plan.ParamsSHA
		case "cancel":
			cancel()
		}
		_, err := transportRepo.Reserve(ctx, scope, digest, size)
		cancel()
		if err == nil {
			t.Fatal("invalid transport reservation", fault)
		}
	}

	wrongPrepared := prepared
	wrongPrepared.params.ID = uuid.New()
	if _, err := wrongPrepared.Descriptor(context.Background()); err == nil {
		t.Fatal("foreign published Params accepted")
	}

	published := v2input.Ref{ArtifactID: uuid.New(), SHA256: plan.ParamsSHA}
	entries, direct, err := PerspectiveTaskInputEntries(context.Background(), plan, sourceRaw, verified, published)
	if err != nil || len(entries) < 5 || len(direct) != 4 {
		t.Fatal(entries, direct, err)
	}
	contracts := map[string]bool{}
	for _, entry := range entries {
		contracts[entry.Contract] = true
	}
	for _, contract := range []string{"Params", "WorkingImage", "Calibration", "Report", "NpyArray"} {
		if !contracts[contract] {
			t.Fatal("missing contract", contract)
		}
	}
	badPlan := plan
	badPlan.ReservedBytes++
	if _, _, err := PerspectiveTaskInputEntries(context.Background(), badPlan, sourceRaw, verified, published); err == nil {
		t.Fatal("changed reservation accepted")
	}
	badParams := published
	badParams.ArtifactID = params.Base.Source.ArtifactID
	if _, _, err := PerspectiveTaskInputEntries(context.Background(), plan, sourceRaw, verified, badParams); err == nil {
		t.Fatal("aliased Params accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompilePerspectiveTaskPlan(cancelled, params.Base, plan.Params, sourceRaw, verified, 200000000, required); err == nil {
		t.Fatal("cancelled compiler accepted")
	}
	params.Corners[0].X = .01
	reportRaw[0] ^= 1
	calibrationRaw[0] ^= 1
	fixture.Locks.RequiredChecks[0] = "changed"
	if err := verified.Verify(); err != nil {
		t.Fatal("caller mutation leaked", err)
	}
	if _, err := CompilePerspectiveManualExecutionProfile(params, sourceRaw, verified, 200000000, required); err == nil {
		t.Fatal("changed intent accepted")
	}
	if _, err := (VerifiedPerspectiveQualification{}).Pin(); err == nil {
		t.Fatal("zero qualification accepted")
	}
}

func mustPerspectiveParams(t *testing.T, p PerspectiveParams) []byte {
	t.Helper()
	body, _, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return body
}
