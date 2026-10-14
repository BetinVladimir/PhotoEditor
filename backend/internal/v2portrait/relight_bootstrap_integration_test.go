package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
	"time"
)

type relightBootstrapObservedDB struct {
	pool        *pgxpool.Pool
	begins      int
	beforeBegin func(int)
}

func (d *relightBootstrapObservedDB) Begin(ctx context.Context) (pgx.Tx, error) {
	d.begins++
	if d.beforeBegin != nil {
		d.beforeBegin(d.begins)
	}
	return d.pool.Begin(ctx)
}

// Fault injection wraps a real committed PostgreSQL transaction. It models
// lost acknowledgement without rolling back or replacing SQL with a double.
type relightUnknownCommitDB struct{ pool *pgxpool.Pool }
type relightUnknownCommitTx struct {
	pgx.Tx
	admitted bool
}

func (d relightUnknownCommitDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &relightUnknownCommitTx{Tx: tx}, nil
}
func (tx *relightUnknownCommitTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "photo.seal_v28_relight_graph(") {
		tx.admitted = true
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}
func (tx *relightUnknownCommitTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	if tx.admitted {
		return errors.New("injected lost commit acknowledgement")
	}
	return nil
}

type relightPublicationObjects struct {
	store    *objectstore.Store
	afterPut func()
	puts     int
}

func (o *relightPublicationObjects) PutImmutable(ctx context.Context, key string, body io.Reader, size int64, sha, media string) (objectstore.Object, error) {
	o.puts++
	obj, err := o.store.PutImmutable(ctx, key, body, size, sha, media)
	if err == nil && o.afterPut != nil {
		o.afterPut()
	}
	return obj, err
}
func (o *relightPublicationObjects) RecoverImmutable(ctx context.Context, key string, size int64, sha, media string) (objectstore.Object, error) {
	return o.store.RecoverImmutable(ctx, key, size, sha, media)
}

func TestIntegrationRelightBootstrap(t *testing.T) {
	path := os.Getenv("PHOTO_TEST_RELIGHT_BOOTSTRAP_SEED")
	if path == "" {
		t.Skip("isolated bootstrap seed required")
	}
	var seed struct {
		Tenant, Project, Snapshot, Run, Workflow, Actor, Session uuid.UUID
		Subject, Source, Params, Report, Calibration             string
		ManualCount                                              int `json:"manual_count"`
		Pin                                                      RelightQualificationPin
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &seed) != nil {
		t.Fatal("invalid bootstrap seed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database configuration")
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE photo_scheduler")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("scheduler pool")
	}
	defer pool.Close()
	admin, err := pgx.Connect(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("control connection")
	}
	defer admin.Close(context.Background())
	store, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET"), AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal("object store")
	}
	objects := &relightObservedObjects{store: store}
	expected := v2qualification.Expected{Operation: "image.relight", Version: "1", Backend: "cpu", ModelLockSHA: seed.Pin.ModelLockSHA, RuntimeLockSHA: seed.Pin.RuntimeLockSHA, DeviceProfileSHA: seed.Pin.DeviceProfileSHA, DatasetSHA: seed.Pin.DatasetSHA, IssuerID: seed.Pin.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("e", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("f", 64): {}}, LicenseCleared: true}
	observed := &relightBootstrapObservedDB{pool: pool}
	coordinator := RelightBootstrapCoordinator{DB: observed, Objects: objects, Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET"), Expected: expected, RasterLimits: RelightManualRasterLimits{PointChecks: 1000000, EdgeEvaluations: 1000000, PayloadBytes: 1000000}, BatchLimits: RelightManualRasterLimits{PointChecks: 2000000, EdgeEvaluations: 2000000, PayloadBytes: 2000000}}
	if os.Getenv("PHOTO_TEST_RELIGHT_RECEIPT_ONLY") == "1" {
		first, e := coordinator.ReadRelightQueuedReceipt(ctx, auth.Identity{UserID: seed.Actor, SessionID: seed.Session}, seed.Tenant, seed.Run)
		if e != nil || len(first) == 0 || objects.calls != 0 {
			t.Fatal("fresh process receipt", e)
		}
		second, e := coordinator.ReadRelightQueuedReceipt(ctx, auth.Identity{UserID: seed.Actor, SessionID: seed.Session}, seed.Tenant, seed.Run)
		if e != nil || string(first) != string(second) || objects.calls != 0 {
			t.Fatal("fresh process receipt replay", e)
		}
		return
	}
	if os.Getenv("PHOTO_TEST_RELIGHT_RESOLVERS_ONLY") == "1" {
		who := auth.Identity{UserID: seed.Actor, SessionID: seed.Session}
		var params RelightParams
		if json.Unmarshal([]byte(seed.Params), &params) != nil {
			t.Fatal("resolver Params")
		}
		evidence, e := LoadRelightQualificationEvidence(ctx, SQLRelightQualificationCatalog{DB: pool, Bucket: coordinator.Bucket}, objects, who, seed.Tenant, seed.Project, params, seed.Pin, expected)
		if e != nil || string(evidence.Report) != seed.Report || string(evidence.Calibration) != seed.Calibration {
			t.Fatal("upgraded qualification resolver", e)
		}
		source, e := (SQLRelightSourceReader{DB: pool, Objects: objects, Bucket: coordinator.Bucket}).Load(ctx, who, seed.Tenant, seed.Project, seed.Snapshot, params.Base)
		if e != nil || string(source) != seed.Source || objects.calls != 3 {
			t.Fatal("upgraded source resolver", e, objects.calls)
		}
		return
	}
	capacity, err := LocalPortraitResources(5, 5)
	if err != nil {
		t.Fatal(err)
	}
	request := RelightBootstrapRequest{Who: auth.Identity{UserID: seed.Actor, SessionID: seed.Session}, Tenant: seed.Tenant, Project: seed.Project, Snapshot: seed.Snapshot, Run: seed.Run, Workflow: seed.Workflow, SubjectKind: seed.Subject, ParamsRaw: []byte(seed.Params), SourceRaw: []byte(seed.Source), RuntimeSHA: seed.Pin.RuntimeLockSHA, Capacity: capacity, Pin: seed.Pin, Evidence: RelightQualificationEvidence{Report: []byte(seed.Report), Calibration: []byte(seed.Calibration)}}
	if os.Getenv("PHOTO_TEST_RELIGHT_SUBMIT_ONLY") == "1" {
		if _, err = admin.Exec(ctx, "UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','200000000') WHERE id=$1", seed.Tenant); err != nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='image.relight' AND version='1'"); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("PHOTO_TEST_RELIGHT_ADMISSION_FAULT") == "unknown_commit" {
			coordinator.DB = relightUnknownCommitDB{pool}
		}
		var params RelightParams
		if json.Unmarshal(request.ParamsRaw, &params) != nil {
			t.Fatal("qualification Params")
		}
		for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_auth", "photo_portal", "photo_publisher", "photo_maintenance"} {
			var allowed bool
			if err = admin.QueryRow(ctx, "SELECT has_function_privilege($1,'photo.v28_relight_qualification_sources(uuid,uuid,uuid,uuid,jsonb)','EXECUTE')", role).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
				t.Fatal("qualification catalog ACL", role, err)
			}
		}
		for _, fault := range []string{"tenant", "project", "actor", "session", "report", "expired_report", "revoked_policy"} {
			who, tenant, project, pin := request.Who, seed.Tenant, seed.Project, seed.Pin
			switch fault {
			case "tenant":
				tenant = uuid.New()
			case "project":
				project = uuid.New()
			case "actor":
				who.UserID = uuid.New()
			case "session":
				who.SessionID = uuid.New()
			case "report":
				pin.Report.ArtifactID = uuid.New()
			case "expired_report":
				if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", pin.Report.ArtifactID); err != nil {
					t.Fatal(err)
				}
			case "revoked_policy":
				if _, err = admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", pin.PolicyID); err != nil {
					t.Fatal(err)
				}
			}
			reads := objects.calls
			got, e := LoadRelightQualificationEvidence(ctx, SQLRelightQualificationCatalog{DB: pool, Bucket: coordinator.Bucket}, objects, who, tenant, project, params, pin, expected)
			if e == nil || got.Report != nil || objects.calls != reads {
				t.Fatal("qualification refusal reached IO", fault, e)
			}
			if fault == "expired_report" {
				if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", pin.Report.ArtifactID); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "revoked_policy" {
				if _, err = admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", pin.PolicyID); err != nil {
					t.Fatal(err)
				}
			}
		}
		loaded, e := LoadRelightQualificationEvidence(ctx, SQLRelightQualificationCatalog{DB: pool, Bucket: coordinator.Bucket}, objects, request.Who, seed.Tenant, seed.Project, params, seed.Pin, expected)
		if e != nil || !bytes.Equal(loaded.Report, request.Evidence.Report) || !bytes.Equal(loaded.Calibration, request.Evidence.Calibration) {
			t.Fatal("server qualification loader", e)
		}
		for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_auth", "photo_portal", "photo_publisher", "photo_maintenance"} {
			var allowed bool
			if err = admin.QueryRow(ctx, "SELECT has_function_privilege($1,'photo.v28_relight_source_document(uuid,uuid,uuid,uuid,uuid,jsonb)','EXECUTE')", role).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
				t.Fatal("source resolver ACL", role, err)
			}
		}
		for _, fault := range []string{"tenant", "project", "snapshot", "session", "source", "bucket", "expired"} {
			who, tenant, project, snapshot, base, bucket := request.Who, seed.Tenant, seed.Project, seed.Snapshot, params.Base, coordinator.Bucket
			switch fault {
			case "tenant":
				tenant = uuid.New()
			case "project":
				project = uuid.New()
			case "snapshot":
				snapshot = uuid.New()
			case "session":
				who.SessionID = uuid.New()
			case "source":
				base.Source.ArtifactID = uuid.New()
			case "bucket":
				bucket = "wrong-bucket"
			case "expired":
				if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", base.Source.ArtifactID); err != nil {
					t.Fatal(err)
				}
			}
			reads := objects.calls
			if raw, e := (SQLRelightSourceReader{DB: pool, Objects: objects, Bucket: bucket}).Load(ctx, who, tenant, project, snapshot, base); e == nil || raw != nil || objects.calls != reads {
				t.Fatal("source refusal reached IO", fault, e)
			}
			if fault == "expired" {
				if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", base.Source.ArtifactID); err != nil {
					t.Fatal(err)
				}
			}
		}
		objects.afterOpen = func() {
			if _, err = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", seed.Session); err != nil {
				t.Fatal(err)
			}
		}
		if raw, e := (SQLRelightSourceReader{DB: pool, Objects: objects, Bucket: coordinator.Bucket}).Load(ctx, request.Who, seed.Tenant, seed.Project, seed.Snapshot, params.Base); e == nil || raw != nil {
			t.Fatal("source returned after session revocation", e)
		}
		objects.afterOpen = nil
		if _, err = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", seed.Session); err != nil {
			t.Fatal(err)
		}
		source, e := (SQLRelightSourceReader{DB: pool, Objects: objects, Bucket: coordinator.Bucket}).Load(ctx, request.Who, seed.Tenant, seed.Project, seed.Snapshot, params.Base)
		if e != nil || !bytes.Equal(source, request.SourceRaw) {
			t.Fatal("server source loading", e)
		}
		request.SourceRaw = source
		request.Evidence = loaded
		request.SourceRaw = []byte("untrusted source bytes")
		request.Evidence = RelightQualificationEvidence{Report: []byte("untrusted report"), Calibration: []byte("untrusted calibration")}
		var receipt []byte
		if os.Getenv("PHOTO_TEST_RELIGHT_SERVICE_ONLY") == "1" {
			service, err := NewRelightSubmissionService(coordinator, store, seed.Tenant, seed.Project, seed.Pin, capacity)
			if err != nil {
				t.Fatal("submission service config", err)
			}
			intentBody, err := canonical.Marshal(relightDraftIntentWire{SchemaVersion: 1, Snapshot: seed.Snapshot, SubjectKind: seed.Subject, Params: request.ParamsRaw})
			if err != nil {
				t.Fatal(err)
			}
			intent, err := DecodeRelightDraftIntent(intentBody)
			if err != nil {
				t.Fatal("public authored intent", err)
			}
			draftRequest, err := service.CreateKeyedDraft(ctx, request.Who, "relight-native-draft", intent)
			if err != nil {
				t.Fatal("create keyed initial relight draft", err)
			}
			seed.Run, seed.Workflow = draftRequest.Run, draftRequest.Workflow
			replayed, err := service.CreateKeyedDraft(ctx, request.Who, "relight-native-draft", intent)
			if err != nil || replayed.Run != seed.Run || replayed.Workflow != seed.Workflow {
				t.Fatal("keyed draft replay", err)
			}
			changedIntent := intent
			changedIntent.Ordinal = 1
			if _, err = service.CreateKeyedDraft(ctx, request.Who, "relight-native-draft", changedIntent); err == nil {
				t.Fatal("key accepted changed intent")
			}
			if err = service.CreateDraft(ctx, request.Who, draftRequest); err != nil {
				t.Fatal("initial draft replay", err)
			}
			changed := draftRequest
			changed.Ordinal = 1
			if err = service.CreateDraft(ctx, request.Who, changed); err == nil {
				t.Fatal("changed initial intent accepted")
			}
			for _, function := range []string{"photo.read_v28_relight_request_receipt(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb)", "photo.create_v28_relight_initial_draft(uuid,uuid,uuid,uuid,text,jsonb,text)", "photo.save_v28_relight_intent(uuid,uuid,jsonb)", "photo.save_v28_relight_intent_pre_initial_draft(uuid,uuid,jsonb)"} {
				for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_auth", "photo_portal", "photo_publisher", "photo_maintenance"} {
					var allowed bool
					want := role == "photo_scheduler" && !strings.Contains(function, "pre_initial_draft")
					if err = admin.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, function).Scan(&allowed); err != nil || allowed != want {
						t.Fatal("initial draft ACL", role, function, allowed, err)
					}
				}
			}
			changedBody, err := canonical.Marshal(relightSubmissionWire{SchemaVersion: 1, Snapshot: seed.Snapshot, Run: seed.Run, Workflow: seed.Workflow, Ordinal: 1, SubjectKind: seed.Subject, Params: request.ParamsRaw})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = service.SubmitJSON(ctx, request.Who, changedBody); err == nil {
				t.Fatal("submission changed frozen draft ordinal")
			}
			var changedParams RelightParams
			if err = json.Unmarshal(request.ParamsRaw, &changedParams); err != nil {
				t.Fatal(err)
			}
			changedParams.Strength = .25
			if original, _, compileErr := changedParams.Compile(); compileErr != nil || string(original) == string(request.ParamsRaw) {
				t.Fatal("changed Params fixture", compileErr)
			}
			changedRaw, _, err := changedParams.Compile()
			if err != nil {
				t.Fatal(err)
			}
			changedBody, err = canonical.Marshal(relightSubmissionWire{SchemaVersion: 1, Snapshot: seed.Snapshot, Run: seed.Run, Workflow: seed.Workflow, SubjectKind: seed.Subject, Params: changedRaw})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = service.SubmitJSON(ctx, request.Who, changedBody); err == nil {
				t.Fatal("submission changed frozen draft Params")
			}
			request.Run, request.Workflow = seed.Run, seed.Workflow
			body, err := canonical.Marshal(relightSubmissionWire{SchemaVersion: 1, Snapshot: seed.Snapshot, Run: seed.Run, Workflow: seed.Workflow, SubjectKind: seed.Subject, Params: request.ParamsRaw})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = DecodeRelightSubmissionRequest(body); err != nil {
				t.Fatal(err)
			}
			receipt, e = service.SubmitKeyed(ctx, request.Who, "relight-native-draft", intent)
			if e == nil {
				replayedReceipt, replayErr := service.SubmitKeyed(ctx, request.Who, "relight-native-draft", intent)
				if replayErr != nil || string(replayedReceipt) != string(receipt) {
					t.Fatal("keyed admission replay", replayErr)
				}
				if rejected, rejectErr := service.SubmitKeyed(ctx, request.Who, "relight-native-draft", changedIntent); rejectErr == nil || rejected != nil {
					t.Fatal("changed keyed admission accepted", rejectErr)
				}
				changedIntent = intent
				changedIntent.ParamsRaw = changedRaw
				if rejected, rejectErr := service.SubmitKeyed(ctx, request.Who, "relight-native-draft", changedIntent); rejectErr == nil || rejected != nil {
					t.Fatal("changed keyed Params accepted", rejectErr)
				}
			}
		} else {
			receipt, e = coordinator.SubmitResolvedRelight(ctx, store, request)
		}

		if e != nil || len(receipt) == 0 {
			t.Fatal("complete server submission", e)
		}
		recovered, e := coordinator.ReadRelightQueuedReceipt(ctx, request.Who, seed.Tenant, seed.Run)
		if e != nil || string(receipt) != string(recovered) {
			t.Fatal("submission receipt recovery", e)
		}
		var count int
		if e = admin.QueryRow(ctx, "SELECT count(*) FROM photo.run_events WHERE run_id=$1 AND type='v2.run.queued'", seed.Run).Scan(&count); e != nil || count != 1 {
			t.Fatal("submission event count", e, count)
		}
		return
	}
	if os.Getenv("PHOTO_TEST_RELIGHT_RESUME_ONLY") == "1" {
		prepared, err := coordinator.Prepare(ctx, request)
		if err != nil {
			t.Fatal("reprepare after upgrade", err)
		}
		frozen, err := coordinator.PublishPreparedRelightTask(ctx, store, prepared)
		if err != nil {
			t.Fatal("restore published task", err)
		}
		if os.Getenv("PHOTO_TEST_RELIGHT_RESUME_ADMISSION_ONLY") == "1" {
			if err = coordinator.AdmitRelightGraph(ctx, prepared, frozen); err == nil {
				t.Fatal("disabled registry admitted upgrade")
			}
			if _, err = admin.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='image.relight' AND version='1'"); err != nil {
				t.Fatal(err)
			}
			if err = coordinator.AdmitRelightGraph(ctx, prepared, frozen); err != nil {
				t.Fatal("admit upgraded registered task", err)
			}
			return
		}
		if os.Getenv("PHOTO_TEST_RELIGHT_RESUME_RECOVERY_ONLY") == "1" {
			reads := objects.calls
			graph, err := coordinator.ReadRelightRegisteredTransport(ctx, prepared, frozen)
			if err != nil || graph.Open == nil || graph.Built.Count < 11 || objects.calls != reads {
				t.Fatal("fresh process SQL-only recovery", err)
			}
			var status string
			var sealed bool
			if err = admin.QueryRow(ctx, "SELECT status,graph_sealed FROM photo.runs WHERE id=$1", seed.Run).Scan(&status, &sealed); err != nil || status != "draft" || sealed {
				t.Fatal("recovery activated run", err)
			}
			return
		}
		graph, err := coordinator.PublishRelightTransport(ctx, store, prepared, frozen)
		if err != nil || graph.Open == nil || graph.Built.Count < 11 {
			t.Fatal("resume transport after upgrade", err)
		}
		if os.Getenv("PHOTO_TEST_RELIGHT_RESUME_REGISTER_ONLY") == "1" {
			if err = coordinator.RegisterRelightTransport(ctx, prepared, frozen, graph); err != nil {
				t.Fatal("register retained transport after upgrade", err)
			}
			var count int
			if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", seed.Tenant, graph.ID).Scan(&count); err != nil || count != graph.Built.Count+len(graph.Built.Chunks)+1 {
				t.Fatal("upgrade registration incomplete", err)
			}
		}
		var status string
		var sealed bool
		if err = admin.QueryRow(ctx, "SELECT status,graph_sealed FROM photo.runs WHERE id=$1", seed.Run).Scan(&status, &sealed); err != nil || status != "draft" || sealed {
			t.Fatal("upgrade resume activated run", err)
		}
		return
	}
	var stored []byte
	if err = admin.QueryRow(ctx, "SELECT v28_relight_intent FROM photo.runs WHERE id=$1", seed.Run).Scan(&stored); err != nil || stored != nil {
		t.Fatal("expected empty draft", err)
	}
	// A committed revocation during reads must leave the draft empty.
	objects.afterOpen = func() {
		if _, err := admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", seed.Session); err != nil {
			t.Fatal("revocation")
		}
	}
	if got, err := coordinator.Prepare(ctx, request); err == nil || got.Intent.ParamsJCS != "" {
		t.Fatal("revoked bootstrap accepted")
	}
	objects.afterOpen = nil
	if _, err = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", seed.Session); err != nil {
		t.Fatal("restore")
	}
	if err = admin.QueryRow(ctx, "SELECT v28_relight_intent FROM photo.runs WHERE id=$1", seed.Run).Scan(&stored); err != nil || stored != nil {
		t.Fatal("failed bootstrap saved partial intent")
	}
	start := observed.begins
	result, err := coordinator.Prepare(ctx, request)
	prepareTransactions := observed.begins - start
	if err != nil {
		t.Fatal("actual bootstrap failed", err)
	}
	if len(result.Producers.Manual) != seed.ManualCount || (result.Producers.Person != nil) != (seed.Subject == "person") {
		t.Fatal("wrong producer grouping")
	}
	if err = admin.QueryRow(ctx, "SELECT v28_relight_intent FROM photo.runs WHERE id=$1", seed.Run).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	normalized, err := canonical.Bytes(stored)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := canonical.Marshal(result.Intent)
	if !bytes.Equal(normalized, encoded) {
		t.Fatal("saved intent mismatch")
	}
	again, err := coordinator.Prepare(ctx, request)
	if err != nil || again.Intent != result.Intent {
		t.Fatal("idempotent bootstrap replay", err)
	}
	// Retained authority is reusable only with its exact private preparation.
	// Mutating returned public document buffers must not corrupt the witness.
	checkPrepared := func(candidate RelightBootstrapResult, wantOK bool) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		err = coordinator.RevalidatePrepared(ctx, tx, candidate)
		if (err == nil) != wantOK {
			t.Fatal("retained prepared authority", wantOK, err)
		}
	}
	checkPrepared(result, true)
	checkPrepared(RelightBootstrapResult{Intent: result.Intent, Documents: result.Documents, Producers: result.Producers}, false)
	changed := result
	changed.Intent.InputsSHA = strings.Repeat("0", 64)
	checkPrepared(changed, false)
	isolated, err := coordinator.Prepare(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, producer := range isolated.Producers.Manual {
		for i := range producer.RootBody {
			producer.RootBody[i] = 'x'
		}
	}
	for i := range isolated.Documents.Source {
		isolated.Documents.Source[i] = 'x'
	}
	checkPrepared(isolated, true)
	if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", result.Producers.Depth.Ref.ArtifactID); err != nil {
		t.Fatal(err)
	}
	checkPrepared(result, false)
	if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", result.Producers.Depth.Ref.ArtifactID); err != nil {
		t.Fatal(err)
	}
	checkPrepared(result, true)
	// Actual quota reservation, immutable S3 PUT, revoked completion, recovery
	// and ready replay. Object IO runs after the reservation transaction commits.
	publicationObjects := &relightPublicationObjects{store: store}
	var quota []byte
	if err = admin.QueryRow(ctx, "SELECT quotas FROM photo.tenants WHERE id=$1", seed.Tenant).Scan(&quota); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','0') WHERE id=$1", seed.Tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = coordinator.PublishPreparedRelightParams(ctx, publicationObjects, result); !errors.Is(err, artifactjson.ErrQuota) || publicationObjects.puts != 0 {
		t.Fatal("quota did not refuse before IO", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.tenants SET quotas=$2::jsonb WHERE id=$1", seed.Tenant, string(quota)); err != nil {
		t.Fatal(err)
	}
	paramsScope := artifactjson.Scope{Tenant: seed.Tenant, Run: seed.Run, Owner: result.Intent.OwnerID, Purpose: "task_params"}
	repository := relightParamsRepository{coordinator: coordinator, prepared: result}
	proof, err := repository.proof(paramsScope, result.Intent.ParamsSHA, int64(len(result.Intent.ParamsJCS)))
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"depth", "subject", "protected", "manual", "extra"} {
		var forged map[string]any
		if json.Unmarshal([]byte(proof), &forged) != nil {
			t.Fatal("proof parse")
		}
		switch fault {
		case "manual":
			forged[fault] = []any{}
		case "extra":
			forged[fault] = true
		default:
			forged[fault] = "{}"
		}
		body, _ := canonical.Marshal(forged)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = coordinator.RevalidatePrepared(ctx, tx, result); err != nil {
			t.Fatal(err)
		}
		var allowed bool
		err = tx.QueryRow(ctx, "SELECT photo.v28_relight_params_document_scope($1,$2,$3,'task_params',$4,$5,$6::jsonb)", seed.Tenant, seed.Run, result.Intent.OwnerID, result.Intent.ParamsSHA, int64(len(result.Intent.ParamsJCS)), string(body)).Scan(&allowed)
		_ = tx.Rollback(context.Background())
		if err != nil || allowed {
			t.Fatal("forged SQL proof accepted", fault, err)
		}
	}
	for _, function := range []string{"photo.read_v28_relight_registered_transport(uuid,uuid,uuid,text,jsonb)", "photo.register_v28_relight_transport(uuid,uuid,text,jsonb,jsonb,text)", "photo.v28_relight_transport_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb,text)", "photo.reserve_v28_relight_transport_document(uuid,uuid,uuid,text,text,bigint,text,jsonb,text)", "photo.complete_v28_relight_transport_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb,text)", "photo.stage_v28_relight_task(uuid,uuid,text,jsonb)", "photo.v28_relight_params_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb)", "photo.reserve_v28_relight_params_document(uuid,uuid,uuid,text,text,bigint,text,jsonb)", "photo.complete_v28_relight_params_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb)"} {
		if os.Getenv("PHOTO_TEST_RELIGHT_REGISTERED_ONLY") == "1" && strings.Contains(function, "read_v28_relight_registered_transport") {
			continue
		}
		if os.Getenv("PHOTO_TEST_RELIGHT_STAGE_ONLY") == "1" && (strings.Contains(function, "relight_transport") || strings.Contains(function, "relight_registered_transport")) {
			continue
		}
		if os.Getenv("PHOTO_TEST_RELIGHT_TRANSPORT_ONLY") == "1" && (strings.Contains(function, "register_v28_relight_transport") || strings.Contains(function, "read_v28_relight_registered_transport")) {
			continue
		}
		for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_auth", "photo_portal", "photo_publisher", "photo_maintenance"} {
			var allowed bool
			if err = admin.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, function).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
				t.Fatal("relight function ACL", role, function, err)
			}
		}
	}
	for _, function := range []string{"photo.seal_v28_relight_graph(uuid,uuid,uuid,text,jsonb)", "photo.read_v28_relight_queued_receipt(uuid,uuid,uuid,uuid)"} {
		var present bool
		if err = admin.QueryRow(ctx, "SELECT to_regprocedure($1) IS NOT NULL", function).Scan(&present); err != nil {
			t.Fatal(err)
		}
		if !present {
			continue
		}
		for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_auth", "photo_portal", "photo_publisher", "photo_maintenance"} {
			var allowed bool
			if err = admin.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, function).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
				t.Fatal("admission/recovery ACL", role, function, err)
			}
		}
	}
	var reservedBefore, storageBefore int64
	if err = admin.QueryRow(ctx, "SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=$1", seed.Tenant).Scan(&reservedBefore, &storageBefore); err != nil {
		t.Fatal(err)
	}
	firstTicket, err := repository.Reserve(ctx, paramsScope, result.Intent.ParamsSHA, int64(len(result.Intent.ParamsJCS)))
	if err != nil {
		t.Fatal(err)
	}
	secondTicket, err := repository.Reserve(ctx, paramsScope, result.Intent.ParamsSHA, int64(len(result.Intent.ParamsJCS)))
	if err != nil || firstTicket.ArtifactID != secondTicket.ArtifactID || firstTicket.Token == secondTicket.Token {
		t.Fatal("reservation resume fencing", err)
	}
	key, err := artifactjson.DocumentKey(paramsScope, firstTicket.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.Complete(ctx, paramsScope, firstTicket, objectstore.Object{Key: key, SHA256: result.Intent.ParamsSHA, Bytes: int64(len(result.Intent.ParamsJCS)), ContentType: "application/json", VersionID: "stale-fence"}); !errors.Is(err, artifactjson.ErrStale) {
		t.Fatal("old token completed", err)
	}
	publicationObjects.afterPut = func() {
		if _, err := admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", seed.Session); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = coordinator.PublishPreparedRelightParams(ctx, publicationObjects, result); err == nil || publicationObjects.puts == 0 {
		t.Fatal("revoked after PUT accepted or PUT not reached", err)
	}
	var state string
	var reservedID uuid.UUID
	if err = admin.QueryRow(ctx, "SELECT state,artifact_id FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2 AND owner_id=$3", seed.Tenant, seed.Run, result.Intent.OwnerID).Scan(&state, &reservedID); err != nil || state != "pending" {
		t.Fatal("revoked completion not pending", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", seed.Session); err != nil {
		t.Fatal(err)
	}
	publicationObjects.afterPut = nil
	published, err := coordinator.PublishPreparedRelightParams(ctx, publicationObjects, result)
	if err != nil || published.ID != reservedID || published.SHA256 != result.Intent.ParamsSHA {
		t.Fatal("actual Params recovery failed", err)
	}
	frozen, err := coordinator.PublishPreparedRelightTask(ctx, publicationObjects, result)
	if err != nil {
		t.Fatal("freeze published complete task", err)
	}
	draft, err := frozen.DraftTask()
	if err != nil || draft.ID != result.Intent.OwnerID || draft.ParamsArtifactID != published.ID || draft.ExecutionProfile == nil {
		t.Fatal("frozen task binding", err)
	}
	var originalEntries []v2input.Entry
	if json.Unmarshal([]byte(result.Intent.InputsJCS), &originalEntries) != nil {
		t.Fatal("saved inputs")
	}
	expectedEntries := make(map[uuid.UUID]v2input.Entry, len(originalEntries)+1)
	for _, entry := range originalEntries {
		expectedEntries[entry.Ref.ArtifactID] = entry
	}
	expectedEntries[published.ID] = v2input.Entry{Ref: v2input.Ref{ArtifactID: published.ID, SHA256: published.SHA256}, Contract: "Params"}
	for pass := 0; pass < 2; pass++ {
		cursor, err := frozen.OpenInputs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for {
			entry, ok, err := cursor.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			if expectedEntries[entry.Ref.ArtifactID] != entry {
				t.Fatal("lost original producer entry")
			}
			count++
		}
		if count != len(expectedEntries) {
			t.Fatal("incomplete frozen producer inventory")
		}
	}
	beforeReplay := publicationObjects.puts
	replayed, err := coordinator.PublishPreparedRelightParams(ctx, publicationObjects, result)
	if err != nil || replayed != published || publicationObjects.puts != beforeReplay {
		t.Fatal("ready replay performed IO", err)
	}
	if err = admin.QueryRow(ctx, "SELECT state FROM photo.backend_documents WHERE artifact_id=$1", published.ID).Scan(&state); err != nil || state != "ready" {
		t.Fatal("Params not ready", err)
	}
	var reservedAfter, storageAfter int64
	if err = admin.QueryRow(ctx, "SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=$1", seed.Tenant).Scan(&reservedAfter, &storageAfter); err != nil || reservedAfter != reservedBefore || storageAfter != storageBefore+int64(len(result.Intent.ParamsJCS)) {
		t.Fatal("Params quota counted more than once", err)
	}
	var status string
	var sealed bool
	var tasks int
	if err = admin.QueryRow(ctx, "SELECT status,graph_sealed,(SELECT count(*) FROM photo.tasks WHERE run_id=$1) FROM photo.runs WHERE id=$1", seed.Run).Scan(&status, &sealed, &tasks); err != nil || status != "draft" || sealed || tasks != 0 {
		t.Fatal("bootstrap activated draft", err)
	}
	// Calibrate the final transaction boundary from the successful production
	// path. Only observation/control connections are wrapped; SQL/IO are actual.
	guardProducer := result.Producers.Manual[len(result.Producers.Manual)-1]
	root, err := review.ParseManualRasterDocument(guardProducer.RootBody, review.ArtifactRef{ID: guardProducer.Root.ArtifactID, SHA256: guardProducer.Root.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"policy", "depth_params", "guard_pixels"} {
		fresh := request
		fresh.Run = uuid.New()
		fresh.Workflow = uuid.New()
		if _, err := admin.Exec(ctx, "INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) SELECT $1,tenant_id,'v2','draft',input_snapshot_id,config_snapshot,config_hash,created_by,'fail_fast',false FROM photo.runs WHERE id=$2", fresh.Run, seed.Run); err != nil {
			t.Fatal("fresh draft", err)
		}
		if _, err := admin.Exec(ctx, "INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) SELECT $1,tenant_id,project_id,$2,input_snapshot_id,workflow_type,renderer_version,policy_sha FROM photo.v2_workflows WHERE id=$3", fresh.Workflow, fresh.Run, seed.Workflow); err != nil {
			t.Fatal("fresh workflow", err)
		}
		finalBegin := observed.begins + prepareTransactions
		applied := false
		observed.beforeBegin = func(n int) {
			if n != finalBegin {
				return
			}
			applied = true
			var err error
			switch fault {
			case "policy":
				_, err = admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", seed.Pin.PolicyID)
			case "depth_params":
				_, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", result.Producers.Depth.Ref.ArtifactID)
			case "guard_pixels":
				_, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", root.Protected.Artifact.ID)
			}
			if err != nil {
				t.Fatal("final-boundary revocation", fault, err)
			}
		}
		failed, err := coordinator.Prepare(ctx, fresh)
		observed.beforeBegin = nil
		if !applied || err == nil || failed.Intent.ParamsJCS != "" {
			t.Fatal("final retained authority missed revocation", fault, err)
		}
		if err = admin.QueryRow(ctx, "SELECT v28_relight_intent FROM photo.runs WHERE id=$1", fresh.Run).Scan(&stored); err != nil || stored != nil {
			t.Fatal("partial intent on final refusal", fault, err)
		}
		checkPrepared(result, false)
		switch fault {
		case "policy":
			_, err = admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", seed.Pin.PolicyID)
		case "depth_params":
			_, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", result.Producers.Depth.Ref.ArtifactID)
		case "guard_pixels":
			_, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", root.Protected.Artifact.ID)
		}
		if err != nil {
			t.Fatal("restore final authority", err)
		}
		checkPrepared(result, true)
		if _, err = coordinator.Prepare(ctx, fresh); err != nil {
			t.Fatal("retry after restore", fault, err)
		}
	}
	// Stage remains blocked and reserves output/transport exactly once. Existing
	// fixture quota deliberately cannot fit the CPU output estimate.
	if err = coordinator.StageRelightTask(ctx, result, frozen); err == nil {
		t.Fatal("stage ignored insufficient output quota")
	}
	var descriptor *string
	if err = admin.QueryRow(ctx, "SELECT v28_relight_descriptor_jcs FROM photo.runs WHERE id=$1", seed.Run).Scan(&descriptor); err != nil || descriptor != nil {
		t.Fatal("quota refusal saved descriptor", err)
	}
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE run_id=$1", seed.Run).Scan(&tasks); err != nil || tasks != 0 {
		t.Fatal("quota refusal staged task", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','200000000') WHERE id=$1", seed.Tenant); err != nil {
		t.Fatal(err)
	}
	canonicalDescriptor, err := frozen.Descriptor()
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"entries", "profile", "digest", "params", "extra"} {
		var forged map[string]any
		if json.Unmarshal(canonicalDescriptor, &forged) != nil {
			t.Fatal("descriptor parse")
		}
		task := forged["task"].(map[string]any)
		switch fault {
		case "entries":
			forged["entries"] = []any{}
		case "profile":
			task["execution_profile"] = map[string]any{"backend": "cpu"}
		case "digest":
			task["input_digest"] = strings.Repeat("a", 64)
		case "params":
			task["params_artifact_id"] = uuid.New().String()
		case "extra":
			forged["extra"] = true
		}
		body, _ := canonical.Marshal(forged)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = coordinator.RevalidatePrepared(ctx, tx, result); err != nil {
			t.Fatal(err)
		}
		var allowed bool
		err = tx.QueryRow(ctx, "SELECT photo.stage_v28_relight_task($1,$2,$3,$4::jsonb)", seed.Tenant, seed.Run, string(body), proof).Scan(&allowed)
		_ = tx.Rollback(context.Background())
		if err != nil || allowed {
			t.Fatal("forged relight descriptor accepted", fault, err)
		}
	}
	if err = coordinator.StageRelightTask(ctx, result, frozen); err != nil {
		t.Fatal("stage complete relight task", err)
	}
	var reservedStaged int64
	if err = admin.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", seed.Tenant).Scan(&reservedStaged); err != nil || reservedStaged != reservedAfter+frozen.reservedBytes+3145728 {
		t.Fatal("stage reservation accounting", err)
	}
	if err = coordinator.StageRelightTask(ctx, result, frozen); err != nil {
		t.Fatal("stage replay", err)
	}
	var reservedReplayed int64
	if err = admin.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", seed.Tenant).Scan(&reservedReplayed); err != nil || reservedReplayed != reservedStaged {
		t.Fatal("stage replay charged quota", err)
	}
	if err = admin.QueryRow(ctx, "SELECT v28_relight_descriptor_jcs FROM photo.runs WHERE id=$1", seed.Run).Scan(&descriptor); err != nil || descriptor == nil || *descriptor != string(canonicalDescriptor) {
		t.Fatal("wrong retained descriptor", err)
	}
	var attempt int
	var inputCount int
	if err = admin.QueryRow(ctx, "SELECT state,attempt,jsonb_array_length(inputs) FROM photo.tasks WHERE tenant_id=$1 AND id=$2", seed.Tenant, draft.ID).Scan(&status, &attempt, &inputCount); err != nil || status != "blocked" || attempt != 0 || inputCount != 0 {
		t.Fatal("stage activated task", err)
	}
	for _, purpose := range []string{"v2_input_chunk_0", "v2_input_chunk_1", "v2_input_manifest"} {
		var outcome string
		err = pool.QueryRow(ctx, "SELECT outcome FROM photo.reserve_v2_input_document($1,$2,$3,$4,$5,2,$6)", seed.Tenant, seed.Run, draft.ID, purpose, strings.Repeat("a", 64), coordinator.Bucket).Scan(&outcome)
		if err != nil || outcome != "forbidden" {
			t.Fatal("generic transport bypassed relight staging", purpose, err)
		}
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND holder_id=$2 AND holder_type='run'", seed.Tenant, seed.Run); err != nil {
		t.Fatal(err)
	}
	if err = coordinator.StageRelightTask(ctx, result, frozen); err == nil {
		t.Fatal("expired stage quota replay accepted")
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()+interval '1 hour' WHERE tenant_id=$1 AND holder_id=$2 AND holder_type='run'", seed.Tenant, seed.Run); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PHOTO_TEST_RELIGHT_STAGE_ONLY") == "1" {
		return
	}
	// Actual dedicated transport publication, revocation after chunk PUT and
	// recovery with the same pinned chunk ID. Generic publication stays denied.
	transportObjects := &relightPublicationObjects{store: store}
	transportObjects.afterPut = func() {
		if _, err := admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", seed.Session); err != nil {
			t.Fatal(err)
		}
	}
	if graph, err := coordinator.PublishRelightTransport(ctx, transportObjects, result, frozen); err == nil || graph.Open != nil || transportObjects.puts != 1 {
		t.Fatal("revoked transport returned graph", err)
	}
	var pendingChunk uuid.UUID
	if err = admin.QueryRow(ctx, "SELECT artifact_id,state FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2 AND owner_id=$3 AND purpose='v2_input_chunk_0'", seed.Tenant, seed.Run, draft.ID).Scan(&pendingChunk, &state); err != nil || state != "pending" {
		t.Fatal("chunk not pending after revocation", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", seed.Session); err != nil {
		t.Fatal(err)
	}
	transportObjects.afterPut = nil
	graph, err := coordinator.PublishRelightTransport(ctx, transportObjects, result, frozen)
	if err != nil || graph.ID != draft.ID || graph.Built.Count != len(expectedEntries) || len(graph.Built.Chunks) != 1 || graph.Built.Chunks[0].ArtifactID != pendingChunk {
		t.Fatal("dedicated transport recovery", err)
	}
	for _, ref := range []v2input.Ref{graph.Built.Chunks[0], graph.Built.Manifest} {
		var obj objectstore.Object
		err = admin.QueryRow(ctx, "SELECT object_key,object_version,sha256,bytes,media_type FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", seed.Tenant, ref.ArtifactID).Scan(&obj.Key, &obj.VersionID, &obj.SHA256, &obj.Bytes, &obj.ContentType)
		if err != nil {
			t.Fatal(err)
		}
		stream, err := store.Open(ctx, obj)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(stream)
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal("read actual transport", readErr, closeErr)
		}
		if ref.ArtifactID == graph.Built.Manifest.ArtifactID {
			var manifest v2input.Manifest
			if json.Unmarshal(body, &manifest) != nil || manifest.TaskID != draft.ID || manifest.EntriesSHA != graph.Built.SHA256 || manifest.EntryCount != len(expectedEntries) {
				t.Fatal("actual manifest mismatch")
			}
		} else {
			var chunk v2input.Chunk
			if json.Unmarshal(body, &chunk) != nil || len(chunk.Entries) != len(expectedEntries) {
				t.Fatal("actual chunk count")
			}
			for _, entry := range chunk.Entries {
				if expectedEntries[entry.Ref.ArtifactID] != entry {
					t.Fatal("actual chunk lost producer")
				}
			}
		}
	}
	beforeTransportReplay := transportObjects.puts
	replayGraph, err := coordinator.PublishRelightTransport(ctx, transportObjects, result, frozen)
	if err != nil || replayGraph.Built.Manifest != graph.Built.Manifest || replayGraph.Built.SHA256 != graph.Built.SHA256 || transportObjects.puts != beforeTransportReplay {
		t.Fatal("transport replay performed IO", err)
	}
	var transportStorage, transportReserved, transportDocumentBytes int64
	if err = admin.QueryRow(ctx, "SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", seed.Tenant).Scan(&transportStorage, &transportReserved); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT sum(bytes) FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2 AND owner_id=$3 AND state='ready' AND purpose IN('v2_input_chunk_0','v2_input_manifest')", seed.Tenant, seed.Run, draft.ID).Scan(&transportDocumentBytes); err != nil {
		t.Fatal(err)
	}
	if transportReserved != reservedStaged || transportStorage != storageAfter+transportDocumentBytes {
		t.Fatal("transport quota not charged exactly once")
	}
	for _, purpose := range []string{"v2_input_chunk_0", "v2_input_chunk_1", "v2_input_manifest"} {
		badBody := "{}"
		badSHA, _ := canonical.Hash(json.RawMessage(badBody))
		scope := artifactjson.Scope{Tenant: seed.Tenant, Run: seed.Run, Owner: draft.ID, Purpose: purpose}
		repo := relightTransportRepository{coordinator: coordinator, prepared: result, body: badBody}
		if _, err := repo.Reserve(ctx, scope, badSHA, 2); !errors.Is(err, artifactjson.ErrStale) {
			t.Fatal("changed transport body accepted", purpose, err)
		}
	}
	if os.Getenv("PHOTO_TEST_RELIGHT_TRANSPORT_ONLY") == "1" {
		return
	}
	manifestBody, _ := canonical.Marshal(v2input.Manifest{SchemaVersion: 1, TaskID: draft.ID, EntryCount: graph.Built.Count, EntriesSHA: graph.Built.SHA256, Chunks: graph.Built.Chunks})
	for _, fault := range []string{"manifest", "chunks", "count", "sha256", "extra"} {
		wire := map[string]any{"manifest": graph.Built.Manifest, "chunks": graph.Built.Chunks, "count": graph.Built.Count, "sha256": graph.Built.SHA256}
		switch fault {
		case "manifest":
			wire[fault] = v2input.Ref{ArtifactID: uuid.New(), SHA256: graph.Built.Manifest.SHA256}
		case "chunks":
			wire[fault] = []v2input.Ref{}
		case "count":
			wire[fault] = graph.Built.Count + 1
		case "sha256":
			wire[fault] = strings.Repeat("0", 64)
		case "extra":
			wire[fault] = true
		}
		body, _ := canonical.Marshal(wire)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = coordinator.RevalidatePrepared(ctx, tx, result); err != nil {
			t.Fatal(err)
		}
		var allowed bool
		err = tx.QueryRow(ctx, "SELECT photo.register_v28_relight_transport($1,$2,$3,$4::jsonb,$5::jsonb,$6)", seed.Tenant, seed.Run, string(canonicalDescriptor), proof, string(body), string(manifestBody)).Scan(&allowed)
		_ = tx.Rollback(context.Background())
		if err != nil || allowed {
			t.Fatal("changed transport registration accepted", fault, err)
		}
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", graph.Built.Manifest.ArtifactID); err != nil {
		t.Fatal(err)
	}
	if err = coordinator.RegisterRelightTransport(ctx, result, frozen, graph); err == nil {
		t.Fatal("expired manifest registered")
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", graph.Built.Manifest.ArtifactID); err != nil {
		t.Fatal(err)
	}
	var noRows int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", seed.Tenant, draft.ID).Scan(&noRows); err != nil || noRows != 0 {
		t.Fatal("failed registration wrote partial rows", err)
	}
	var genericCount int
	if err = pool.QueryRow(ctx, "SELECT photo.register_v2_task_input_batch($1,$2,$3,0,'[]'::jsonb)", seed.Tenant, seed.Run, draft.ID).Scan(&genericCount); err == nil || !strings.Contains(err.Error(), "RELIGHT_DEDICATED_REGISTRATION_REQUIRED") {
		t.Fatal("generic batch bypassed relight", err)
	}
	if os.Getenv("PHOTO_TEST_RELIGHT_REGISTERED_ONLY") != "1" {
		if recovered, err := coordinator.ReadRelightRegisteredTransport(ctx, result, frozen); err == nil || recovered.Open != nil {
			t.Fatal("unregistered recovery accepted", err)
		}
	}
	// Caller cursor is deliberately hostile; registration must use frozen data.
	graph.Open = func(context.Context) (v2input.Cursor, error) { t.Fatal("caller cursor used"); return nil, nil }
	if err = coordinator.RegisterRelightTransport(ctx, result, frozen, graph); err != nil {
		t.Fatal("register exact relight transport", err)
	}
	if err = coordinator.RegisterRelightTransport(ctx, result, frozen, graph); err != nil {
		t.Fatal("registered transport replay", err)
	}
	if os.Getenv("PHOTO_TEST_RELIGHT_REGISTERED_ONLY") == "1" {
		return
	}
	beforeRecoveryReads, beforeRecoveryPuts := objects.calls, transportObjects.puts
	recovered, err := coordinator.ReadRelightRegisteredTransport(ctx, result, frozen)
	if err != nil || recovered.Built.Manifest != graph.Built.Manifest || recovered.Built.SHA256 != graph.Built.SHA256 || recovered.Open == nil || objects.calls != beforeRecoveryReads || transportObjects.puts != beforeRecoveryPuts {
		t.Fatal("SQL-only registered recovery", err)
	}
	recovered.Direct[0] = v2input.Ref{}
	cursor, err := recovered.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok, err := cursor.Next(ctx)
	if err != nil || !ok || expectedEntries[entry.Ref.ArtifactID] != entry {
		t.Fatal("recovered cursor borrowed roots", err)
	}
	for _, ref := range []v2input.Ref{graph.Built.Manifest, graph.Built.Chunks[0]} {
		if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", ref.ArtifactID); err != nil {
			t.Fatal(err)
		}
		if refused, err := coordinator.ReadRelightRegisteredTransport(ctx, result, frozen); err == nil || refused.Open != nil {
			t.Fatal("expired registered transport recovered", err)
		}
		if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", ref.ArtifactID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = coordinator.ReadRelightRegisteredTransport(ctx, result, frozen); err != nil {
		t.Fatal("recovery after expiry restored", err)
	}
	var genericSealed bool
	if err = pool.QueryRow(ctx, "SELECT photo.seal_v2_graph($1,$2)", seed.Tenant, seed.Run).Scan(&genericSealed); err != nil || genericSealed {
		t.Fatal("generic graph admission bypassed relight", err)
	}
	var registeredRows int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", seed.Tenant, draft.ID).Scan(&registeredRows); err != nil || registeredRows != len(expectedEntries)+len(graph.Built.Chunks)+1 {
		t.Fatal("registered inventory incomplete", err)
	}
	if err = admin.QueryRow(ctx, "SELECT state,attempt,jsonb_array_length(inputs) FROM photo.tasks WHERE tenant_id=$1 AND id=$2", seed.Tenant, draft.ID).Scan(&status, &attempt, &inputCount); err != nil || status != "blocked" || attempt != 0 || inputCount != 8 {
		t.Fatal("registration activated or lost roots", err)
	}
	if os.Getenv("PHOTO_TEST_RELIGHT_ADMISSION") == "1" {
		if _, e := admin.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='image.relight' AND version='1'"); e != nil {
			t.Fatal(e)
		}
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", seed.Tenant.String()); e != nil {
			t.Fatal(e)
		}
		var admitted bool
		e = tx.QueryRow(ctx, "SELECT photo.seal_v28_relight_graph($1,$2,$3,$4,$5::jsonb)", seed.Tenant, seed.Run, draft.ID, string(canonicalDescriptor), proof).Scan(&admitted)
		if e != nil || !admitted {
			tx.Rollback(ctx)
			t.Fatal("dedicated relight admission", e)
		}
		if _, e = tx.Exec(ctx, "RESET ROLE"); e != nil {
			t.Fatal(e)
		}
		var receipt bool
		e = tx.QueryRow(ctx, "SELECT graph_sealed AND v28_relight_queued_receipt->>'state'='queued' FROM photo.runs WHERE id=$1", seed.Run).Scan(&receipt)
		if e != nil || !receipt {
			tx.Rollback(ctx)
			t.Fatal("missing atomic receipt", e)
		}
		if e = tx.Rollback(ctx); e != nil {
			t.Fatal(e)
		}
	}
	before := objects.calls
	if _, err = admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", seed.Pin.PolicyID); err != nil {
		t.Fatal("policy revocation")
	}
	checkPrepared(result, false)
	if g, e := coordinator.ReadRelightRegisteredTransport(ctx, result, frozen); e == nil || g.Open != nil {
		t.Fatal("revoked recovery accepted", e)
	}
	if err = coordinator.RegisterRelightTransport(ctx, result, frozen, graph); err == nil {
		t.Fatal("revoked registration replay accepted")
	}
	if g, e := coordinator.PublishRelightTransport(ctx, transportObjects, result, frozen); e == nil || g.Open != nil || transportObjects.puts != beforeTransportReplay {
		t.Fatal("revoked transport replay accepted", e)
	}
	if err = coordinator.StageRelightTask(ctx, result, frozen); err == nil {
		t.Fatal("revoked stage replay accepted")
	}
	beforeReplay = publicationObjects.puts
	if _, err = coordinator.PublishPreparedRelightParams(ctx, publicationObjects, result); err == nil || publicationObjects.puts != beforeReplay {
		t.Fatal("revoked ready replay accepted", err)
	}
	if got, err := coordinator.Prepare(ctx, request); err == nil || got.Intent.ParamsJCS != "" || objects.calls != before {
		t.Fatal("revoked policy reached IO")
	}
	if os.Getenv("PHOTO_TEST_RELIGHT_ADMISSION") == "1" {
		if err = coordinator.AdmitRelightGraph(ctx, result, frozen); err == nil {
			t.Fatal("revoked admission accepted")
		}
		if _, err = admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", seed.Pin.PolicyID); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("PHOTO_TEST_RELIGHT_ADMISSION_FAULT") == "cursor_exhausted" {
			var prior int64
			if _, err = admin.Exec(ctx, "INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES($1,0) ON CONFLICT DO NOTHING", seed.Tenant); err != nil {
				t.Fatal(err)
			}
			if err = admin.QueryRow(ctx, "SELECT last_cursor FROM photo.tenant_event_cursors WHERE tenant_id=$1", seed.Tenant).Scan(&prior); err != nil {
				t.Fatal(err)
			}
			if _, err = admin.Exec(ctx, "UPDATE photo.tenant_event_cursors SET last_cursor=9007199254740991 WHERE tenant_id=$1", seed.Tenant); err != nil {
				t.Fatal(err)
			}
			if err = coordinator.AdmitRelightGraph(ctx, result, frozen); err == nil || !strings.Contains(err.Error(), "EVENT_CURSOR_EXHAUSTED") {
				t.Fatal("cursor failure not surfaced", err)
			}
			var intact bool
			if err = admin.QueryRow(ctx, "SELECT NOT graph_sealed AND status='draft' AND v28_relight_queued_receipt IS NULL FROM photo.runs WHERE id=$1", seed.Run).Scan(&intact); err != nil || !intact {
				t.Fatal("failed admission changed run", err)
			}
			if err = admin.QueryRow(ctx, "SELECT state='blocked' AND attempt=0 FROM photo.tasks WHERE id=$1", draft.ID).Scan(&intact); err != nil || !intact {
				t.Fatal("failed admission activated task", err)
			}
			if _, err = admin.Exec(ctx, "UPDATE photo.tenant_event_cursors SET last_cursor=$2 WHERE tenant_id=$1", seed.Tenant, prior); err != nil {
				t.Fatal(err)
			}
		}
		switch os.Getenv("PHOTO_TEST_RELIGHT_ADMISSION_FAULT") {
		case "unknown_commit":
			faulted := coordinator
			faulted.DB = relightUnknownCommitDB{pool}
			if e := faulted.AdmitRelightGraph(ctx, result, frozen); e == nil || e.Error() != "injected lost commit acknowledgement" {
				t.Fatal("missing commit fault", e)
			}
		case "concurrent":
			concurrent := coordinator
			concurrent.DB = pool
			start := make(chan struct{})
			results := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func() { <-start; results <- concurrent.AdmitRelightGraph(ctx, result, frozen) }()
			}
			close(start)
			wins := 0
			for i := 0; i < 2; i++ {
				e := <-results
				if e == nil {
					wins++
				} else if !errors.Is(e, v2input.ErrExpired) {
					t.Fatal("concurrent admission error", e)
				}
			}
			if wins != 1 {
				t.Fatal("concurrent admission winner count", wins)
			}
		default:
			if err = coordinator.AdmitRelightGraph(ctx, result, frozen); err != nil {
				t.Fatal("commit admission", err)
			}
		}
		var events int
		if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.run_events WHERE run_id=$1 AND type='v2.run.queued'", seed.Run).Scan(&events); err != nil || events != 1 {
			t.Fatal("duplicate queued events", err, events)
		}
		if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND routing_key='v2.run.queued' AND payload->>'run_id'=$2", seed.Tenant, seed.Run.String()).Scan(&events); err != nil || events != 1 {
			t.Fatal("duplicate queued outbox", err, events)
		}

		var receipt bool
		if err = admin.QueryRow(ctx, "SELECT graph_sealed AND v28_relight_queued_receipt->>'state'='queued' FROM photo.runs WHERE id=$1", seed.Run).Scan(&receipt); err != nil || !receipt {
			t.Fatal("committed receipt absent", err)
		}
		if err = admin.QueryRow(ctx, "SELECT state,attempt FROM photo.tasks WHERE id=$1", draft.ID).Scan(&status, &attempt); err != nil || status != "ready" || attempt != 0 {
			t.Fatal("committed task not ready", err, status, attempt)
		}
		if err = coordinator.AdmitRelightGraph(ctx, result, frozen); err == nil {
			t.Fatal("blind admission replay accepted")
		}
		if os.Getenv("PHOTO_TEST_RELIGHT_QUEUED_ONLY") == "1" {
			return
		}
		first, e := coordinator.ReadRelightQueuedReceipt(ctx, request.Who, seed.Tenant, seed.Run)
		if e != nil || len(first) == 0 {
			t.Fatal("queued receipt recovery", e)
		}
		if _, e = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", seed.Session); e != nil {
			t.Fatal(e)
		}
		if raw, e := coordinator.ReadRelightQueuedReceipt(ctx, request.Who, seed.Tenant, seed.Run); e == nil || raw != nil {
			t.Fatal("revoked receipt recovered")
		}
		if _, e = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", seed.Session); e != nil {
			t.Fatal(e)
		}
		second, e := coordinator.ReadRelightQueuedReceipt(ctx, request.Who, seed.Tenant, seed.Run)
		if e != nil || string(first) != string(second) {
			t.Fatal("receipt changed on replay", e)
		}
	}
}
