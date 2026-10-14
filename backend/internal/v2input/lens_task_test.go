package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestPrepareLensTaskBuildsReplayableTransportBeforeCommit(t *testing.T) {
	ref := func() Ref { return Ref{uuid.New(), strings.Repeat("a", 64)} }
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	_ = json.Unmarshal(raw, &fixtures)
	body := fixtures[0]
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	canvas, _ := canonical.Hash(image.Canvas)
	digest := sha256.Sum256(body)
	binding := SessionVersionBinding{uuid.New(), Ref{uuid.New(), hex.EncodeToString(digest[:])}, canvas}
	params := OpticalLensParams{1, binding, 1, ref(), 50, 2}

	lens := LensProfileDocument{SchemaVersion: 1, CameraModel: "Camera", LensModel: "Lens", FocalMM: 50, Aperture: 2, CX: .5, CY: .5,
		K: []float64{0, 0, 0}, P: []float64{0, 0}, Vignette: []float64{0, 0, 0}, CARedScale: 1, CABlueScale: 1, Qualification: ref()}
	profileBody, _ := canonical.Marshal(lens)
	profileSHA, _ := canonical.Hash(lens)
	params.Profile = Ref{uuid.New(), profileSHA}
	psha, _ := canonical.Hash(params)
	original := LensOriginalBinding{binding, Ref{uuid.New(), image.Canvas.SourceSHA256}, ".jpg"}
	osha, _ := canonical.Hash(original)
	request := LensTaskRequest{Workflow: uuid.New(), Ordinal: 0, Params: params, ParamsRef: Ref{uuid.New(), psha},
		Original: original, OriginalRef: Ref{uuid.New(), osha}, ProfileJSON: profileBody, SourceJSON: body, OriginalBytes: 100, RAMLimit: 32 << 30, ScratchLimit: 32 << 30, OutputLimit: 32 << 30}

	frozen, err := CompileLensTask(request)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := frozen.DocumentPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.ParamsSHA != request.ParamsRef.SHA256 || plan.OriginalSHA != request.OriginalRef.SHA256 || plan.Original != request.Original {
		t.Fatal(plan)
	}
	psum := sha256.Sum256([]byte(plan.ParamsJCS))
	osum := sha256.Sum256([]byte(plan.OriginalJCS))
	if hex.EncodeToString(psum[:]) != plan.ParamsSHA || hex.EncodeToString(osum[:]) != plan.OriginalSHA {
		t.Fatal("scope content hashes disagree")
	}

	docs := &scopedDocuments{}
	tenant, run := uuid.New(), uuid.New()
	published, err := PublishLensDocuments(context.Background(), docs, tenant, run, plan)
	if err != nil || published.Params.SHA256 != plan.ParamsSHA || published.Original.SHA256 != plan.OriginalSHA || len(docs.scopes) != 2 {
		t.Fatal(published, err)
	}
	for _, scope := range docs.scopes {
		if scope.Tenant != tenant || scope.Run != run || scope.Owner != plan.TaskID {
			t.Fatal(scope)
		}
	}
	if docs.scopes[0].Purpose != "v25_lens_original_binding" || docs.scopes[1].Purpose != "v25_lens_params" {
		t.Fatal(docs.scopes)
	}
	wrongPlan := plan
	wrongPlan.ParamsJCS = "{}"
	untouchedDocs := &scopedDocuments{}
	if _, err = PublishLensDocuments(context.Background(), untouchedDocs, tenant, run, wrongPlan); err == nil || len(untouchedDocs.scopes) != 0 {
		t.Fatal("altered plan published")
	}

	finalRequest := request
	finalRequest.ParamsRef = published.Params
	finalRequest.OriginalRef = published.Original
	finalFrozen, err := CompileLensTask(finalRequest)
	if err != nil {
		t.Fatal(err)
	}
	finalDraft, err := finalFrozen.DraftTask()
	if err != nil || finalDraft.ParamsArtifactID != published.Params.ArtifactID || finalDraft.ID != plan.TaskID {
		t.Fatal(finalDraft, err)
	}
	finalPlan, err := finalFrozen.DocumentPlan()
	if err != nil || finalPlan.ParamsJCS != plan.ParamsJCS || finalPlan.OriginalJCS != plan.OriginalJCS {
		t.Fatal("publication changed frozen content", err)
	}
	haveParams, haveOriginal := false, false
	for _, ref := range finalFrozen.closure.Direct {
		haveParams = haveParams || ref == published.Params
		haveOriginal = haveOriginal || ref == published.Original
	}
	if !haveParams || !haveOriginal {
		t.Fatal("published document IDs omitted from final closure")
	}

	stageDB := &lensStageDB{accepted: true}
	stage := SQLLensTaskStage{DB: stageDB}
	if err = stage.Stage(context.Background(), tenant, run, request.Workflow, 0, finalFrozen); err != nil || stageDB.calls != 1 {
		t.Fatal(err)
	}
	if err = stage.Stage(context.Background(), tenant, run, uuid.New(), 0, finalFrozen); err == nil || stageDB.calls != 1 {
		t.Fatal("foreign workflow reached stage")
	}
	stageDB.accepted = false
	if err = stage.Stage(context.Background(), tenant, run, request.Workflow, 0, finalFrozen); err != ErrExpired {
		t.Fatal(err)
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	if draft.Operation != "optical.lens" || draft.NodeKey != "optical.lens:0" || draft.ResourceClass != "cpu" || draft.ParamsArtifactID != request.ParamsRef.ArtifactID {
		t.Fatal(draft)
	}
	// Caller mutation cannot alter the immutable resource profile used after shell commit.
	draft.ExecutionProfile[0] = '!'
	safe, err := frozen.DraftTask()
	if err != nil || !json.Valid(safe.ExecutionProfile) {
		t.Fatal("draft profile aliases frozen state")
	}
	if _, err = (FrozenLensTask{}).DraftTask(); err == nil {
		t.Fatal("empty frozen task accepted")
	}
	publisher := &memoryPublisher{bodies: map[uuid.UUID][]byte{}}
	prepared, err := PrepareLensTask(context.Background(), request, publisher)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := TaskID(request.Workflow, "optical.lens", 0)
	if prepared.Graph.ID != expected || prepared.NodeKey != "optical.lens:0" || prepared.Graph.Built.Count != 7 ||
		!validBuilt(prepared.Graph.Built, prepared.Graph.Direct) || len(publisher.bodies) != 2 {
		t.Fatal(prepared)
	}

	separated, err := PublishLensTask(context.Background(), frozen, publisher)
	if err != nil {
		t.Fatal(err)
	}
	if separated.InputDigest != prepared.InputDigest || separated.Graph.ID != draft.ID {
		t.Fatal("two-phase path differs")
	}
	again, err := PrepareLensTask(context.Background(), request, publisher)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := canonical.Hash(prepared.Graph.Built)
	after, _ := canonical.Hash(again.Graph.Built)
	if before != after {
		t.Fatal("replay changed transport")
	}
	bad := &memoryPublisher{bodies: map[uuid.UUID][]byte{}, bad: true}
	if _, err = PrepareLensTask(context.Background(), request, bad); err == nil {
		t.Fatal("bad publication SHA accepted")
	}

	for _, mutate := range []func(*LensTaskRequest){
		func(r *LensTaskRequest) {
			r.ProfileJSON = append([]byte(nil), r.ProfileJSON...)
			r.ProfileJSON[0] = '!'
		},
		func(r *LensTaskRequest) {
			r.SourceJSON = append([]byte(nil), r.SourceJSON...)
			r.SourceJSON[0] = '!'
		},
		func(r *LensTaskRequest) { r.Params.Base.CanvasHash = strings.Repeat("c", 64) },
		func(r *LensTaskRequest) {
			r.Original.Original.SHA256 = strings.Repeat("0", 64)
			if r.Original.Original.SHA256 == image.Canvas.SourceSHA256 {
				r.Original.Original.SHA256 = strings.Repeat("1", 64)
			}
		},
	} {
		altered := request
		mutate(&altered)
		pub := &memoryPublisher{bodies: map[uuid.UUID][]byte{}}
		if _, err = PrepareLensTask(context.Background(), altered, pub); err == nil || len(pub.bodies) != 0 {
			t.Fatal("source substitution published inputs")
		}
	}

	coordinatorRequest := request
	coordinatorRequest.Workflow, _ = WorkflowID(run)

	configTask, err := CompileLensTask(coordinatorRequest)
	if err != nil {
		t.Fatal(err)
	}
	project, snapshot, session := uuid.New(), uuid.New(), uuid.New()
	config, err := CompileLensDraftConfig(project, snapshot, session, run, []FrozenLensTask{configTask})
	if err != nil || config.Config.QueueClass!="batch" || config.OutputBytes <= 0 || config.Config.Lens.AuthSessionID != session || len(config.Config.Lens.Nodes) != 1 {
		t.Fatal(config, err)
	}
	draftDB := &lensDraftDB{accepted: true}
	store := SQLLensDraftStore{DB: draftDB}
 wrongQueue:=config
 wrongQueue.Config.QueueClass="interactive"
 wrongQueue.Body,_=canonical.Marshal(wrongQueue.Config)
 wrongQueue.SHA256,_=canonical.Hash(wrongQueue.Config)
 if err:=store.Begin(context.Background(),tenant,uuid.New(),run,strings.Repeat("e",64),wrongQueue);err==nil||draftDB.calls!=0{t.Fatal("foreign queue reached lens SQL")}

	if err := store.Begin(context.Background(), tenant, uuid.New(), run, strings.Repeat("e", 64), config); err != nil {
		t.Fatal(err)
	}
	changed := config
	changed.Body = json.RawMessage(`{}`)
	if err := store.Begin(context.Background(), tenant, uuid.New(), run, strings.Repeat("e", 64), changed); err == nil || draftDB.calls != 1 {
		t.Fatal("altered config reached database")
	}
	draftDB.accepted = false
	if err := store.Begin(context.Background(), tenant, uuid.New(), run, strings.Repeat("e", 64), config); err != ErrExpired {
		t.Fatal("admission refusal ignored", err)
	}
	configHash := sha256.Sum256(config.Body)
	if hex.EncodeToString(configHash[:]) != config.SHA256 {
		t.Fatal("config SHA does not bind bytes")
	}
	if _, err = CompileLensDraftConfig(project, snapshot, session, run, []FrozenLensTask{configTask, configTask}); err == nil {
		t.Fatal("duplicate tasks in config accepted")
	}
	if _, err = CompileLensDraftConfig(project, snapshot, session, uuid.New(), []FrozenLensTask{configTask}); err == nil {
		t.Fatal("foreign run task accepted")
	}
	coordinatorDocs := &replayableLensDocuments{}
	coordinatorStage := &lensStageDB{accepted: true}
	registrar := &lensRegistrarFake{}
	coordinator := LensPreparationCoordinator{Documents: coordinatorDocs, Stage: SQLLensTaskStage{DB: coordinatorStage}, Inputs: registrar}
	coordinated, err := coordinator.Resume(context.Background(), tenant, run, coordinatorRequest)
	if err != nil || registrar.calls != 1 || coordinatorStage.calls != 1 || len(coordinatorDocs.scopes) != 4 || coordinated.Graph.ID == uuid.Nil {
		t.Fatal("coordinator phases failed", err)
	}

	repeated, err := coordinator.Resume(context.Background(), tenant, run, coordinatorRequest)
	if err != nil || repeated.Params != coordinated.Params || repeated.InputDigest != coordinated.InputDigest || repeated.Graph.Built.Manifest != coordinated.Graph.Built.Manifest || registrar.calls != 2 {
		t.Fatal("coordinator retry changed task transport", err)
	}
	coordinatorStage.accepted = false
	coordinatorDocs.scopes = nil
	if _, err = coordinator.Resume(context.Background(), tenant, run, coordinatorRequest); err != ErrExpired || len(coordinatorDocs.scopes) != 2 || registrar.calls != 2 {
		t.Fatal("stage failure did not stop transport", err)
	}
	coordinatorDocs.scopes = nil
	coordinatorRequest.Workflow = uuid.New()
	if _, err = coordinator.Resume(context.Background(), tenant, run, coordinatorRequest); err == nil || len(coordinatorDocs.scopes) != 0 {
		t.Fatal("wrong workflow published documents")
	}
	actor := uuid.New()
	batchRun, _ := RunID(tenant, actor, "lens-batch")
	batchRequest := request
	batchRequest.Workflow, _ = WorkflowID(batchRun)
	batchRequest.Ordinal = 0
	batchDocs := &replayableLensDocuments{}
	batchStage := &lensStageDB{accepted: true}
	batchRegistrar := &lensRegistrarFake{}
	batchStore := &lensDraftStoreFake{}
	batch := LensDraftCoordinator{Store: batchStore, Preparation: LensPreparationCoordinator{Documents: batchDocs, Stage: SQLLensTaskStage{DB: batchStage}, Inputs: batchRegistrar}}
	first, err := batch.Prepare(context.Background(), tenant, actor, project, snapshot, session, "lens-batch", strings.Repeat("e", 64), []LensTaskRequest{batchRequest})
	if err != nil || first.RunID != batchRun || len(first.Tasks) != 1 || batchStore.calls != 1 {
		t.Fatal("batch preparation", err)
	}
	second, err := batch.Prepare(context.Background(), tenant, actor, project, snapshot, session, "lens-batch", strings.Repeat("e", 64), []LensTaskRequest{batchRequest})
	if err != nil || first.Tasks[0].InputDigest != second.Tasks[0].InputDigest || first.Tasks[0].Params != second.Tasks[0].Params {
		t.Fatal("batch replay", err)
	}
	sealDB := &lensSealDB{accepted: true}
	if err := (SQLLensDraftStore{DB: sealDB}).Seal(context.Background(), tenant, first); err != nil || sealDB.calls != 1 {
		t.Fatal("prepared graph seal", err)
	}
	wrongDraft := first
	wrongDraft.WorkflowID = uuid.New()
	if err := (SQLLensDraftStore{DB: sealDB}).Seal(context.Background(), tenant, wrongDraft); err == nil || sealDB.calls != 1 {
		t.Fatal("foreign workflow reached seal")
	}
	sealDB.accepted = false
	if err := (SQLLensDraftStore{DB: sealDB}).Seal(context.Background(), tenant, first); err != ErrInvalid {
		t.Fatal("graph guard refusal ignored", err)
	}
	batchScopesBefore := len(batchDocs.scopes)
	invalid := batchRequest
	invalid.Ordinal = 2
	if _, err := batch.Prepare(context.Background(), tenant, actor, project, snapshot, session, "lens-batch", strings.Repeat("e", 64), []LensTaskRequest{batchRequest, invalid}); err == nil || batchStore.calls != 2 || len(batchDocs.scopes) != batchScopesBefore {
		t.Fatal("invalid batch performed writes")
	}
	batchStore.err = ErrExpired
	if _, err := batch.Prepare(context.Background(), tenant, actor, project, snapshot, session, "lens-batch", strings.Repeat("e", 64), []LensTaskRequest{batchRequest}); err != ErrExpired || len(batchDocs.scopes) != batchScopesBefore {
		t.Fatal("refused admission published documents", err)
	}
	batchStore.err = nil
	batchStage.accepted = false
	if _, err := batch.Prepare(context.Background(), tenant, actor, project, snapshot, session, "lens-batch", strings.Repeat("e", 64), []LensTaskRequest{batchRequest}); err != ErrExpired || batchRegistrar.calls != 2 {
		t.Fatal("stage failure registered inputs", err)
	}
	batchStage.accepted = true
	if _, err := batch.Prepare(context.Background(), tenant, actor, project, snapshot, session, "lens-batch", strings.Repeat("e", 64), []LensTaskRequest{batchRequest}); err != nil || batchRegistrar.calls != 3 {
		t.Fatal("failed batch could not resume", err)
	}
	request.RAMLimit = 1
	untouched := &memoryPublisher{bodies: map[uuid.UUID][]byte{}}
	if _, err = PrepareLensTask(context.Background(), request, untouched); err == nil || len(untouched.bodies) != 0 {
		t.Fatal("underbudget published transport")
	}
}

type lensStageDB struct {
	accepted bool
	calls    int
}

func (d *lensStageDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.calls++
	if !strings.Contains(query, "stage_v25_lens_task") || len(args) != 8 {
		return analysisStageRow(false)
	}
	return analysisStageRow(d.accepted)
}

type lensRegistrarFake struct{ calls int }

func (r *lensRegistrarFake) RegisterLensInputs(_ context.Context, _ uuid.UUID, _ uuid.UUID, prepared PreparedLensTask) error {
	if !validBuilt(prepared.Graph.Built, prepared.Graph.Direct) {
		return ErrInvalid
	}
	r.calls++
	return nil
}

// Mirrors immutable scoped publication: retries recover the same artifact ID.
type replayableLensDocuments struct {
	scopes []artifactjson.Scope
	hashes map[uuid.UUID]string
}

func (d *replayableLensDocuments) Publish(_ context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	if d.hashes == nil {
		d.hashes = map[uuid.UUID]string{}
	}
	id := uuid.NewSHA1(scope.Owner, []byte(scope.Run.String()+":"+scope.Purpose))
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if prior, ok := d.hashes[id]; ok && prior != digest {
		return artifactjson.Ref{}, ErrInvalid
	}
	d.hashes[id] = digest
	d.scopes = append(d.scopes, scope)
	return artifactjson.Ref{ID: id, SHA256: digest}, nil
}

// Exercises the scheduler call boundary; authority/quota are checked in PostgreSQL.
type lensDraftDB struct {
	accepted bool
	calls    int
}

func (d *lensDraftDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.calls++
	if !strings.Contains(query, "begin_v25_lens_draft") || len(args) != 10 {
		return analysisStageRow(false)
	}
	return analysisStageRow(d.accepted)
}

type lensDraftStoreFake struct {
	calls int
	err   error
}

func (s *lensDraftStoreFake) Begin(_ context.Context, tenant, actor, run uuid.UUID, policy string, draft CompiledLensDraft) error {
	s.calls++
	if tenant == uuid.Nil || actor == uuid.Nil || run == uuid.Nil || draft.OutputBytes < 1 {
		return ErrInvalid
	}
	return s.err
}

type lensSealDB struct {
	accepted bool
	calls    int
}

func (d *lensSealDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.calls++
	if !strings.Contains(query, "seal_v2_graph") || len(args) != 2 {
		return analysisStageRow(false)
	}
	return analysisStageRow(d.accepted)
}
