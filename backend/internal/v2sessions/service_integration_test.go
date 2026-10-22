package v2sessions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/pagination"
)

type calibrationStore struct{ body []byte }

type calibrationBarrierStore struct { calibrationStore; afterOpen func() }
func (s calibrationBarrierStore) Open(ctx context.Context, object objectstore.Object) (io.ReadCloser,error) {
 reader,err:=s.calibrationStore.Open(ctx,object)
 if s.afterOpen!=nil { s.afterOpen() }
 return reader,err
}

func (s calibrationStore) Open(_ context.Context, _ objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.body)), nil
}

func TestIntegrationSessionCreateReadUpdate(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with V22 migration required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	var actor, tenantID, projectID, collectionID, calibrationID, reportID uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&actor, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", actor).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	authSession := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", authSession, actor, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name) VALUES($1,'V22') RETURNING id", tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Set') RETURNING id", tenantID, projectID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	lock := strings.Repeat("a", 64)
	calibration, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "model_lock_sha": lock, "dataset_sha": strings.Repeat("b", 64),
		"qualified": true, "scene_cosine_min": .3, "scene_log_luma_max": 1,
		"cluster_diameter_max": 1, "sharpness_scale": 1, "clipping_penalty": 1,
		"match_inlier_min": .3, "match_error_px_max": 5,
		"identity_similarity_min": .4, "landmark_drift_max": .2,
		"normalization_scales": []float64{1, 1, 1, 1, 1, 1},
		"loss_weights":         []float64{.4, .1, .1, .1, .2, .1},
		"focus_warning":        .5, "focus_critical": .2, "eye_open_min": .5,
		"glare_warning_fraction": .1, "clipping_warning_fraction": .1,
		"clipping_critical_fraction": .3, "min_face_edge_px": 64,
		"depth_sigma": .1, "registration_error_px": 2, "motion_threshold": .1,
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(calibration)
	digest := hex.EncodeToString(sum[:])
	if err = admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,
	 sha256,bytes,media_type,object_version,verified_at)
	 VALUES($1,'test','calibration',$2,$3,'application/json','v1',now()) RETURNING id`,
		tenantID, digest, len(calibration)).Scan(&calibrationID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,
	 sha256,bytes,media_type,object_version,verified_at)
	 VALUES($1,'test','qualification',$2,2,'application/json','v1',now()) RETURNING id`,
		tenantID, strings.Repeat("c", 64)).Scan(&reportID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.v2_qualification_policies(
	 operation,operation_version,model_lock_sha,runtime_lock_sha,backend,
	 device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,
	 evidence_shas,license_cleared,enabled,admission_enabled)
	 VALUES('session.describe','1',$1,$2,'cuda',$3,$4,$5,
	 ARRAY['quality'],ARRAY[$6],ARRAY[$7],true,true,true)
 ON CONFLICT (operation,operation_version) WHERE admission_enabled DO NOTHING`,
		lock, strings.Repeat("e", 64), strings.Repeat("d", 64),
		strings.Repeat("b", 64), actor, strings.Repeat("f", 64),
		strings.Repeat("1", 64)); err != nil {
		t.Fatal(err)
	}
	var compatible bool
	if err = admin.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.v2_qualification_policies
 WHERE operation='session.describe' AND operation_version='1' AND admission_enabled
 AND model_lock_sha=$1 AND runtime_lock_sha=$2 AND backend='cuda'
 AND device_profile_sha=$3 AND dataset_sha=$4 AND enabled AND license_cleared)`,
		lock, strings.Repeat("e", 64), strings.Repeat("d", 64), strings.Repeat("b", 64)).Scan(&compatible); err != nil || !compatible {
		t.Fatalf("isolated fixture requires compatible synthetic admission policy: compatible=%v err=%v", compatible, err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.model_qualifications(tenant_id,project_id,
	 operation,operation_version,model_lock_sha,runtime_lock_sha,backend,
	 device_profile_sha,dataset_sha,report_artifact_id,report_sha,status,trusted_import)
	 VALUES($1,$2,'session.describe','1',$3,$4,'cuda',$5,$6,$7,$8,'passed',true)`,
		tenantID, projectID, lock, strings.Repeat("e", 64),
		strings.Repeat("d", 64), strings.Repeat("b", 64), reportID,
		strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	service := Service{DB: api, Objects: calibrationStore{body: calibration}, Bucket: "test",
		Cursors: pagination.Codec{Key: []byte(strings.Repeat("k", 32))}}
	who := auth.Identity{UserID: actor, SessionID: authSession, Epoch: epoch}
	policy := SessionPolicy{SchemaVersion: 1, TimeGapSeconds: 60,
		NeighborCount: 4, ProtectManualPicks: true, CameraOffsets: []CameraOffset{},
		Calibration: ArtifactRef{ArtifactID: calibrationID, SHA256: digest}}
	request := CreateRequest{ProjectID: projectID, CollectionID: collectionID,
		Name: "Session", Policy: policy, CapturePolicy: []byte("null")}
	// A valid session qualification must not implicitly authorize Capture.
	captureRequest := request
	captureRequest.CapturePolicy, _ = json.Marshal(CapturePolicy{SchemaVersion: 1, Checks: []string{"focus", "clipping"}, MaxQueueAgeSeconds: 30, Calibration: policy.Calibration})
	rejected, e := service.Create(ctx, who, tenantID, uuid.NewString(), captureRequest)
	if domain.AsError(e).Code != "CAPTURE_UNAVAILABLE" || rejected.ResourceID != nil {
		t.Fatalf("unqualified Capture admission accepted: response=%v error=%v", rejected, e)
	}
	var rejectedRows int
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM photo.shoot_sessions WHERE tenant_id=$1 AND project_id=$2`, tenantID, projectID).Scan(&rejectedRows); e != nil || rejectedRows != 0 {
		t.Fatalf("rejected admission retained session: rows=%d err=%v", rejectedRows, e)
	}
	// Synthetic Capture qualification is committed only in the explicitly isolated test DB.
	qualificationTx, e := admin.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer qualificationTx.Rollback(context.Background())
	if _, e = qualificationTx.Exec(ctx, `INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,evidence_shas,license_cleared,enabled,admission_enabled)
 VALUES('capture.assess','1',$1,$2,'cuda',$3,$4,$5,ARRAY['quality'],ARRAY[$6],ARRAY[$7],true,true,true)
 ON CONFLICT (operation,operation_version) WHERE admission_enabled DO NOTHING`, lock, strings.Repeat("e", 64), strings.Repeat("d", 64), strings.Repeat("b", 64), actor, strings.Repeat("f", 64), strings.Repeat("1", 64)); e != nil {
		t.Fatal(e)
	}
	var captureReport uuid.UUID
	if e = qualificationTx.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES($1,'test',$2,$3,2,'application/json','v1',now()) RETURNING id`, tenantID, "capture-qualification/"+uuid.NewString(), strings.Repeat("c", 64)).Scan(&captureReport); e != nil {
		t.Fatal(e)
	}
	if _, e = qualificationTx.Exec(ctx, `INSERT INTO photo.model_qualifications(tenant_id,project_id,operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,report_artifact_id,report_sha,status,trusted_import)
 VALUES($1,$2,'capture.assess','1',$3,$4,'cuda',$5,$6,$7,$8,'passed',true)`, tenantID, projectID, lock, strings.Repeat("e", 64), strings.Repeat("d", 64), strings.Repeat("b", 64), captureReport, strings.Repeat("c", 64)); e != nil {
		t.Fatal(e)
	}
	if _, e = qualificationTx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenantID.String()); e != nil {
		t.Fatal(e)
	}
	if _, e = qualificationTx.Exec(ctx, `SET LOCAL ROLE photo_api`); e != nil {
		t.Fatal(e)
	}
	capturePolicy := &CapturePolicy{SchemaVersion: 1, Checks: []string{"focus", "clipping"}, MaxQueueAgeSeconds: 30, Calibration: policy.Calibration}
	if e = service.captureReady(ctx, qualificationTx, tenantID, projectID, capturePolicy); e != nil {
		t.Fatal("qualified Capture calibration rejected", e)
	}
	changed := service
	changed.Objects = calibrationStore{body: append(append([]byte{}, calibration...), byte(' '))}
	if e = changed.captureReady(ctx, qualificationTx, tenantID, projectID, capturePolicy); domain.AsError(e).Code != "CALIBRATION_UNAVAILABLE" {
		t.Fatal("changed calibration bytes accepted", e)
	}
	for _, field := range []string{"model_lock_sha", "dataset_sha", "qualified"} {
		var document map[string]any
		if e = json.Unmarshal(calibration, &document); e != nil {
			t.Fatal(e)
		}
		if field == "qualified" {
			document[field] = false
		} else {
			document[field] = strings.Repeat("9", 64)
		}
		raw, e := canonical.Marshal(document)
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(raw)
		badSHA := hex.EncodeToString(sum[:])
		var badArtifact uuid.UUID
		if _, e = qualificationTx.Exec(ctx, `RESET ROLE`); e != nil {
			t.Fatal(e)
		}
		if e = qualificationTx.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES($1,'test',$2,$3,$4,'application/json','v1',now()) RETURNING id`, tenantID, "mismatch/"+uuid.NewString(), badSHA, len(raw)).Scan(&badArtifact); e != nil {
			t.Fatal(e)
		}
		if _, e = qualificationTx.Exec(ctx, `SET LOCAL ROLE photo_api`); e != nil {
			t.Fatal(e)
		}
		mismatched := service
		mismatched.Objects = calibrationStore{body: raw}
		mismatchedPolicy := *capturePolicy
		mismatchedPolicy.Calibration = ArtifactRef{ArtifactID: badArtifact, SHA256: badSHA}
		if e = mismatched.captureReady(ctx, qualificationTx, tenantID, projectID, &mismatchedPolicy); domain.AsError(e).Code != "CALIBRATION_UNAVAILABLE" {
			t.Fatalf("accepted calibration with mismatched %s: %v", field, e)
		}
	}
	if _, e = qualificationTx.Exec(ctx, `RESET ROLE`); e != nil {
		t.Fatal(e)
	}
	if e = qualificationTx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	defer func() {
		// Disable only the policy inserted by this fixture, never another issuer's policy.
		if _, err := admin.Exec(context.Background(), `UPDATE photo.v2_qualification_policies SET enabled=false,admission_enabled=false WHERE operation='capture.assess' AND operation_version='1' AND issuer_id=$1`, actor); err != nil {
			t.Error("disable synthetic capture admission", err)
		}
	}()
	key := uuid.NewString()
	created, err := service.Create(ctx, who, tenantID, key, request)
	if err != nil || created.Status != 201 || created.ResourceID == nil {
		t.Fatal("create session", created, err)
	}
	replay, err := service.Create(ctx, who, tenantID, key, request)
	if err != nil || !replay.Replayed || !bytes.Equal(created.Body, replay.Body) {
		t.Fatal("create replay", replay, err)
	}
	got, err := service.Get(ctx, who, tenantID, *created.ResourceID)
	if err != nil || !bytes.Contains(got, []byte(`"Session"`)) {
		t.Fatal("get session", string(got), err)
	}
	page, err := service.List(ctx, who, tenantID, projectID, 10, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatal("list session", page, err)
	}
	updated, err := service.Update(ctx, who, tenantID, *created.ResourceID,
		uuid.NewString(), UpdateRequest{ExpectedRevision: "1", Name: "Closed",
			State: "closed", Policy: policy, CapturePolicy: []byte("null")})
	if err != nil || updated.Status != 200 || !bytes.Contains(updated.Body, []byte(`"Closed"`)) {
		t.Fatal("update session", updated, err)
	}
	captureKey := uuid.NewString()
	accepted, e := service.Create(ctx, who, tenantID, captureKey, captureRequest)
	if e != nil || accepted.Status != 201 || accepted.ResourceID == nil {
		t.Fatal("full qualified capture create", accepted, e)
	}
	repeated, e := service.Create(ctx, who, tenantID, captureKey, captureRequest)
	if e != nil || !repeated.Replayed || !bytes.Equal(accepted.Body, repeated.Body) {
		t.Fatal("qualified capture replay", repeated, e)
	}
	var sessionBody struct {
		Data struct {
			CapturePolicy *CapturePolicy `json:"capture_policy"`
		} `json:"data"`
		CapturePolicy *CapturePolicy `json:"capture_policy"`
		Revision      string         `json:"revision"`
	}
	if e = json.Unmarshal(accepted.Body, &sessionBody); e != nil || sessionBody.Data.CapturePolicy == nil || sessionBody.Data.CapturePolicy.Calibration != policy.Calibration {
		t.Fatal("capture policy binding", sessionBody, e)
	}
	advisory,e:=service.CaptureCapabilities(ctx,who,tenantID,*accepted.ResourceID,sessionBody.Revision)
	if e!=nil||!advisory.Available||advisory.TemplateID!=*accepted.ResourceID { t.Fatal("qualified template advisory",advisory,e) }
	withdrawn:=false
	late:=service
	late.Objects=calibrationBarrierStore{calibrationStore:calibrationStore{body:calibration},afterOpen:func(){
	 if withdrawn{return};withdrawn=true
	 if _,err:=admin.Exec(ctx,`UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`,tenantID,projectID);err!=nil { t.Fatal(err) }
	}}
	if _,e=late.CaptureCapabilities(ctx,who,tenantID,*accepted.ResourceID,sessionBody.Revision);e==nil { t.Fatal("advisory disclosed after project withdrawal during calibration IO") }
	if !withdrawn { t.Fatal("fixture did not reach actual calibration IO") }
	if _,e=admin.Exec(ctx,`UPDATE photo.projects SET archived_at=NULL WHERE tenant_id=$1 AND id=$2`,tenantID,projectID);e!=nil { t.Fatal(e) }
	closeRequest := UpdateRequest{ExpectedRevision: sessionBody.Revision, Name: "Capture closed", State: "closed", Policy: policy, CapturePolicy: captureRequest.CapturePolicy}
	closed, e := service.Update(ctx, who, tenantID, *accepted.ResourceID, uuid.NewString(), closeRequest)
	if e != nil || closed.Status != 200 {
		t.Fatal("qualified capture close", closed, e)
	}
	if _, e = service.Update(ctx, who, tenantID, *accepted.ResourceID, uuid.NewString(), closeRequest); domain.AsError(e).Code != "REVISION_CONFLICT" {
		t.Fatal("stale capture update accepted", e)
	}
	var count int
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM photo.shoot_sessions WHERE tenant_id=$1 AND project_id=$2 AND capture_policy IS NOT NULL`, tenantID, projectID).Scan(&count); e != nil || count != 1 {
		t.Fatal("capture create replay cardinality", count, e)
	}

}
