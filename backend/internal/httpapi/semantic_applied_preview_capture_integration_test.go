package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticreview"
	"strings"
	"testing"
	"time"
)

func TestIntegrationShownAppliedPreviewCapture(t *testing.T) {
	dsn, raw := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_TEST_V27_SHOWN_TASK")
	if dsn == "" || raw == "" {
		t.Skip("completed owned semantic exposure fixture required")
	}
	task, err := uuid.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	owner, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	operation := os.Getenv("PHOTO_TEST_V27_SHOWN_OPERATION")
	if operation == "" {
		operation = "semantic.adopt_shown_exposure"
	}
	if operation != "semantic.adopt_shown_exposure" && operation != "semantic.adopt_shown_retouch_region" && operation != "semantic.adopt_shown_removal" && operation != "semantic.adopt_shown_background_cleanup" {
		t.Fatal("unsupported shown capture operation")
	}
	var tenant, intent, target, child, actor, session uuid.UUID
	var epoch int64
	err = owner.QueryRow(ctx, `SELECT t.tenant_id,i.id,target.id,t.run_id,r.created_by,(parent.config_snapshot->'v27_semantic'->>'auth_session_id')::uuid,u.auth_epoch FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id JOIN photo.semantic_targets target ON target.tenant_id=t.tenant_id AND target.child_run_id=r.id JOIN photo.semantic_intents i ON i.tenant_id=target.tenant_id AND i.id=target.intent_id JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id AND w.id=i.workflow_id JOIN photo.runs parent ON parent.tenant_id=w.tenant_id AND parent.id=w.run_id JOIN photo.users u ON u.id=r.created_by WHERE t.id=$1 AND t.operation=$2 AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL AND r.status='completed' AND target.state='applied'`, task, operation).Scan(&tenant, &intent, &target, &child, &actor, &session, &epoch)
	if err != nil {
		t.Fatal(err)
	}
	makePool := func(role string) *pgxpool.Pool {
		cfg, e := pgxpool.ParseConfig(dsn)
		if e != nil {
			t.Fatal(e)
		}
		cfg.ConnConfig.RuntimeParams["role"] = role
		p, e := pgxpool.NewWithConfig(ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(p.Close)
		return p
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := &auth.Service{DB: makePool("photo_auth"), Pepper: make([]byte, 32), Signer: auth.TokenSigner{Active: "fixture", Private: priv, Public: map[string]ed25519.PublicKey{"fixture": pub}, Issuer: "fixture", Audience: "fixture"}}
	token, err := service.Signer.Issue(actor, session, epoch)
	if err != nil {
		t.Fatal(err)
	}
	bucket := os.Getenv("PHOTO_TEST_MINIO_BUCKET")
	objects, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal("object fixture unavailable")
	}
	api := makePool("photo_api")
	icc, err := filepath.Abs("../../../resources/icc")
	if err != nil {
		t.Fatal(err)
	}
	pins, err := colorprofile.LoadSHAs(icc)
	if err != nil {
		t.Fatal(err)
	}
	diagnostic := &shownPreviewDiagnosticQuery{db: api}
	applied, err := semanticreview.NewShownAppliedPreviewService(diagnostic, objects, objects, bucket, pins["srgb_v1"])
	if err != nil {
		t.Fatal(err)
	}
	handler := (&Server{SemanticAppliedPreview: applied, DB: api, Auth: service, Config: config.Config{Environment: "development", AppOrigin: "http://localhost:8080", PortalOrigin: "http://localhost:3000", MaxJSONBytes: 65536}}).Routes()
	path := "/api/v1/tenants/" + tenant.String() + "/semantic-targets/" + target.String() + "/preview"
	get := func(url, access string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", url, nil)
		if access != "" {
			r.Header.Set("Authorization", "Bearer "+access)
		}
		r.Header.Set("X-Photo-Client", "native")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	capture := os.Getenv("PHOTO_TEST_V27_APPLIED_PREVIEW_RESPONSE_FILE")
	if capture == "" {
		t.Fatal("actual shown response capture path required")
	}
	if w := get(path, ""); w.Code != 401 {
		t.Fatal("anonymous preview accepted", w.Code)
	}
	w := get(path, token)
	if w.Code != 200 {
		diagnoseShownPreview(t, ctx, owner, diagnostic, tenant, target, actor, session, task)
		t.Fatal("actual shown preview refused", w.Code, w.Body.String())
	}
	var result semanticreview.ShownAppliedPreview
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.SchemaVersion != 1 || result.TenantID != tenant || result.TargetID != target || result.ChildRunID != child || result.After.MediaType != "image/png" || !result.After.ExpiresAt.After(time.Now()) {
		t.Fatal("shown response identity invalid")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", result.After.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal("signed shown preview unavailable")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	closeErr := response.Body.Close()
	sum := sha256.Sum256(body)
	if readErr != nil || closeErr != nil || response.StatusCode != 200 || len(body) == 0 || hex.EncodeToString(sum[:]) != result.After.SHA256 {
		t.Fatal("signed shown preview integrity invalid")
	}
	assertShownPhysicalReceiptNegatives(t, ctx, owner, diagnostic, tenant, target, actor, session, task)
	// Preserve the real HTTP payload; never relabel legacy Run.Outputs.
	captured, err := json.Marshal(map[string]any{"tenant": tenant, "target": target, "run": child, "response": json.RawMessage(w.Body.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(capture, captured, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("V27_SHOWN_APPLIED_PREVIEW_HTTP_OK")
}

// Diagnostic draft: wrap ONLY the service's existing API RowQuerier. The actual
// unchanged GET runs first; this wrapper records stages, never changes results.
type shownPreviewDiagnosticQuery struct {
	db                   *pgxpool.Pool
	stages               []string
	params, presentation string
}
type shownPreviewDiagnosticRow struct {
	row   pgx.Row
	owner *shownPreviewDiagnosticQuery
	stage string
}

func (q *shownPreviewDiagnosticQuery) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	stage := "applied-proof"
	if strings.Contains(query, "semantic_shown_applied_preview_object") && len(args) == 7 {
		stage, _ = args[4].(string)
		if value, ok := args[5].(string); ok && value != "" {
			q.params = value
		}
		if value, ok := args[6].(string); ok && value != "" {
			q.presentation = value
		}
	}
	return shownPreviewDiagnosticRow{row: q.db.QueryRow(ctx, query, args...), owner: q, stage: stage}
}
func (r shownPreviewDiagnosticRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	state := "ok"
	if errors.Is(err, pgx.ErrNoRows) {
		state = "no_rows"
	} else if err != nil {
		state = fmt.Sprintf("error:%T", err)
	}
	r.owner.stages = append(r.owner.stages, r.stage+":"+state)
	return err
}

// Invoke only AFTER the unchanged actual GET has failed. Do not log bodies,
// object keys, URLs, bearer tokens or database credentials.
func diagnoseShownPreview(t *testing.T, ctx context.Context, db *pgxpool.Pool, q *shownPreviewDiagnosticQuery, tenant, target, actor, session, task uuid.UUID) {
	t.Helper()
	t.Logf("APPLIED_PREVIEW_STAGES %v", q.stages)
	var proof, params string
	err := db.QueryRow(ctx, `SELECT COALESCE((SELECT row_to_json(p)::text FROM photo.semantic_shown_applied_image($1,$2,$3,$4) p),'null')`, tenant, target, actor, session).Scan(&proof)
	t.Logf("APPLIED_PREVIEW_PROOF value=%s err=%v", proof, err)
	// owner-only read is a diagnostic, NOT API authorization evidence.
	err = db.QueryRow(ctx, `SELECT jsonb_build_object('params_ready',a.state='ready','params_verified',a.verified_at IS NOT NULL,'params_bytes',a.bytes,'params_sha',a.sha256,'params_registered',EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='Params'))::text FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id WHERE t.tenant_id=$1 AND t.id=$2`, tenant, task).Scan(&params)
	t.Logf("APPLIED_PREVIEW_PARAMS facts=%s err=%v", params, err)
	// The original pipeline captured its verified Params before requesting
	// presentation. If it failed earlier, do not invent a replacement body.
	if q.params == "" {
		t.Log("APPLIED_PREVIEW_NO_VERIFIED_PARAMS: stop nested diagnosis")
		return
	}
	var facts string
	err = db.QueryRow(ctx, `WITH body AS(SELECT $3::jsonb b)
 SELECT jsonb_build_object('preview_task',p.id,'preview_state',p.state,'preview_attempt_matches',p.attempt=(b->>'preview_attempt')::integer,'preview_fence_matches',p.fencing_token=(b->>'preview_fence')::bigint,
 'canonical_result_matches',p.canonical_result_id=(b->>'preview_result_id')::uuid,
 'canonical_outputs_match',z.outputs=jsonb_build_array(b->'presentation'),
 'preview_adoption_inbox',EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p.tenant_id AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(p.id,z.id)),
 'preview_run_completed',r.status='completed','preview_run_sealed',r.graph_sealed,'preview_run_not_cancelled',NOT r.cancel_requested,
 'physical_receipt_present',r.v27_preapply_preview_receipt IS NOT NULL,
 'receipt_task',r.v27_preapply_preview_receipt->>'task_id'=p.id::text,
 'receipt_result',r.v27_preapply_preview_receipt->>'result_id'=z.id::text,
 'receipt_attempt',r.v27_preapply_preview_receipt->>'attempt'=p.attempt::text,
 'receipt_fence',r.v27_preapply_preview_receipt->>'fence'=p.fencing_token::text,
 'receipt_params_sha',r.v27_preapply_preview_receipt->>'params_sha'=r.config_snapshot->'semantic_preapply_preview'->>'params_sha',
 'receipt_presentation',r.v27_preapply_preview_receipt->'presentation'=b->'presentation',
 'receipt_working_image',r.v27_preapply_preview_receipt->'working_image'=b->'working_image',
 'receipt_pixels',r.v27_preapply_preview_receipt->'pixels'=b->'pixels',
 'receipt_evidence',r.v27_preapply_preview_receipt->'numerical_evidence'=b->'numerical_evidence')::text
 FROM body JOIN photo.tasks p ON p.tenant_id=$1 AND p.id=(b->>'preview_task_id')::uuid
 JOIN photo.runs r ON r.tenant_id=p.tenant_id AND r.id=p.run_id
 JOIN photo.task_results z ON z.tenant_id=p.tenant_id AND z.id=p.canonical_result_id
 WHERE b->>'target_id'=$2::text`, tenant, target, q.params).Scan(&facts)
	t.Logf("APPLIED_PREVIEW_NESTED facts=%s err=%v", facts, err)
}

// Draft invocation AFTER genuine successful HTTP GET + signed PNG integrity:
// assertShownPhysicalReceiptNegatives(t,ctx,owner,diagnostic,tenant,target,actor,session,task)
// These are owner-adversarial, rollback-only row corruptions, not new valid
// receipts. No trigger, RLS, grant or immutability mechanism is disabled.
func assertShownPhysicalReceiptNegatives(t *testing.T, ctx context.Context, owner *pgxpool.Pool, q *shownPreviewDiagnosticQuery, tenant, target, actor, session, task uuid.UUID) {
	t.Helper()
	if q.params == "" || q.presentation == "" {
		t.Fatal("genuine physically read documents required")
	}
	var params struct {
		PreviewRunID  uuid.UUID `json:"preview_run_id"`
		PreviewTaskID uuid.UUID `json:"preview_task_id"`
	}
	if err := json.Unmarshal([]byte(q.params), &params); err != nil || params.PreviewRunID == uuid.Nil {
		t.Fatal("missing genuine preview Params identity")
	}
	probe := func(tx pgx.Tx) int {
		t.Helper()
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
			t.Fatal(err)
		}
		var count int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM photo.semantic_shown_applied_preview_object($1,$2,$3,$4,'after',$5,$6)`, tenant, target, actor, session, q.params, q.presentation).Scan(&count)
		if _, resetErr := tx.Exec(ctx, "RESET ROLE"); resetErr != nil {
			t.Fatal(resetErr)
		}
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	// Independent ordinary savepoint transactions restore original valid rows.
	check := func(name, query string, args ...any) {
		t.Helper()
		tx, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if probe(tx) != 1 {
			t.Fatalf("%s: valid baseline absent", name)
		}
		changed, err := tx.Exec(ctx, query, args...)
		if err != nil {
			t.Fatalf("%s: ordinary fixture mutation refused; do not bypass protection: %v", name, err)
		}
		if changed.RowsAffected() != 1 {
			t.Fatalf("%s: mutation count=%d", name, changed.RowsAffected())
		}
		if probe(tx) != 0 {
			t.Fatalf("%s: corrupted receipt/authority accepted", name)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		control, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer control.Rollback(context.Background())
		if probe(control) != 1 {
			t.Fatalf("%s: rollback did not restore valid baseline", name)
		}
		if err := control.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Closed identifiers only; selected from actual persisted operation.
	var operation string
	if err := owner.QueryRow(ctx, "SELECT operation FROM photo.tasks WHERE tenant_id=$1 AND id=$2", tenant, task).Scan(&operation); err != nil {
		t.Fatal(err)
	}
	receipt, configKey, wrongColumn := "", "", ""
	switch operation {
	case "semantic.adopt_shown_exposure":
		receipt = "v27_preapply_preview_receipt"
		configKey = "semantic_preapply_preview"
		wrongColumn = "v27_retouch_preview_receipt"
	case "semantic.adopt_shown_removal":
		receipt = "v27_removal_preview_receipt"
		configKey = "semantic_removal_preview"
		wrongColumn = "v27_preapply_preview_receipt"
	case "semantic.adopt_shown_background_cleanup":
		receipt = "v27_background_cleanup_preview_receipt"
		configKey = "semantic_background_cleanup_preview"
		wrongColumn = "v27_preapply_preview_receipt"
	case "semantic.adopt_shown_retouch_region":
		receipt = "v27_retouch_preview_receipt"
		configKey = "semantic_retouch_preview"
		wrongColumn = "v27_preapply_preview_receipt"
	default:
		t.Fatal("unsupported physical receipt negative family")
	}
	check("missing-receipt", "UPDATE photo.runs SET "+receipt+"=NULL WHERE tenant_id=$1 AND id=$2", tenant, params.PreviewRunID)
	// Each immutable identity/ref field must independently matter. Exact UUIDs
	// need not name a foreign accepted graph: this vector tests corrupted proof.
	for _, field := range []string{"schema_version", "params_sha", "task_id", "result_id", "attempt", "fence", "presentation", "working_image", "pixels", "numerical_evidence"} {
		check("missing-"+field, "UPDATE photo.runs SET "+receipt+"="+receipt+"-$3::text WHERE tenant_id=$1 AND id=$2", tenant, params.PreviewRunID, field)
	}
	check("both-params-sha-missing", "UPDATE photo.runs SET "+receipt+"="+receipt+"-'params_sha',config_snapshot=config_snapshot#-'{"+configKey+",params_sha}' WHERE tenant_id=$1 AND id=$2", tenant, params.PreviewRunID)
	for _, field := range []string{"task_id", "result_id", "attempt", "fence"} {
		value := "999999999"
		if field == "task_id" || field == "result_id" {
			value = uuid.New().String()
		}
		check("wrong-identity-"+field, "UPDATE photo.runs SET "+receipt+"=jsonb_set("+receipt+",ARRAY[$3::text],to_jsonb($4::text)) WHERE tenant_id=$1 AND id=$2", tenant, params.PreviewRunID, field, value)
	}
	for _, field := range []string{"attempt", "fence"} {
		check("same-value-wrong-type-"+field, "UPDATE photo.runs SET "+receipt+"=jsonb_set("+receipt+",ARRAY[$3::text],to_jsonb("+receipt+"->>$3::text)) WHERE tenant_id=$1 AND id=$2", tenant, params.PreviewRunID, field)
	}
	check("malformed-schema-type", "UPDATE photo.runs SET "+receipt+"=jsonb_set("+receipt+",'{schema_version}','\"1\"'::jsonb) WHERE tenant_id=$1 AND id=$2", tenant, params.PreviewRunID)
	check("wrong-schema-version", "UPDATE photo.runs SET "+receipt+"=jsonb_set("+receipt+",'{schema_version}','2'::jsonb) WHERE tenant_id=$1 AND id=$2", tenant, params.PreviewRunID)
	check("wrong-params-sha", "UPDATE photo.runs SET "+receipt+"=jsonb_set("+receipt+",'{params_sha}',to_jsonb(repeat('0',64))) WHERE tenant_id=$1 AND id=$2", tenant, params.PreviewRunID)
	for _, field := range []string{"presentation", "working_image", "pixels", "numerical_evidence"} {
		check("same-id-wrong-sha-"+field, "UPDATE photo.runs SET "+receipt+"=jsonb_set("+receipt+",ARRAY[$3::text,'sha256'],to_jsonb(repeat('0',64))) WHERE tenant_id=$1 AND id=$2", tenant, params.PreviewRunID, field)
	}
	check("wrong-family-column", "UPDATE photo.runs SET "+wrongColumn+"="+receipt+","+receipt+"=NULL WHERE tenant_id=$1 AND id=$2", tenant, params.PreviewRunID)
	check("missing-child-inbox", `DELETE FROM photo.inbox WHERE tenant_id=$1 AND consumer='v2-adopt' AND event_id=(SELECT photo.v2_adoption_event_id(id,canonical_result_id) FROM photo.tasks WHERE tenant_id=$1 AND id=$2)`, tenant, task)
	check("current-session-revoked", `UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, session, actor)
	check("current-project-archived", `UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=(SELECT project_id FROM photo.semantic_targets WHERE tenant_id=$1 AND id=$2) AND archived_at IS NULL`, tenant, target)
	// These revoke the actual persisted session/project; owner membership is
	// untouched and each transaction rolls back before the next valid control.
}
