package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/capability"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/pagination"
	runservice "photoeditor/backend/internal/run"
	"photoeditor/backend/internal/semanticreview"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIntegrationCompletedSemanticTargetsHTTP(t *testing.T) {
	dsn, raw := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_TEST_V27_GATEWAY_TASK")
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
	var tenant, intent, target, child, actor, session uuid.UUID
	var epoch int64
	err = owner.QueryRow(ctx, `SELECT t.tenant_id,i.id,target.id,t.run_id,r.created_by,(parent.config_snapshot->'v27_semantic'->>'auth_session_id')::uuid,u.auth_epoch FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id JOIN photo.semantic_targets target ON target.tenant_id=t.tenant_id AND target.child_run_id=r.id JOIN photo.semantic_intents i ON i.tenant_id=target.tenant_id AND i.id=target.intent_id JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id AND w.id=i.workflow_id JOIN photo.runs parent ON parent.tenant_id=w.tenant_id AND parent.id=w.run_id JOIN photo.users u ON u.id=r.created_by WHERE t.id=$1 AND r.status='completed' AND target.state='applied'`, task).Scan(&tenant, &intent, &target, &child, &actor, &session, &epoch)
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
	intentReader, err := semanticreview.NewPlanObjectReader(objects, bucket)
	if err != nil {
		t.Fatal(err)
	}
	kindReader, err := semanticreview.SQLReviewKindReader(api, intentReader)
	if err != nil {
		t.Fatal(err)
	}
	// Install the real capability-serving origin before Routes captures run service configuration.
	outputServer := httptest.NewUnstartedServer(nil)
	outputOrigin := "http://" + outputServer.Listener.Addr().String()
	handler := (&Server{SemanticReviewKind: kindReader, DB: api, Auth: service, Media: &media.Service{Objects: objects, Bucket: bucket}, Config: config.Config{Environment: "development", APIPublicOrigin: outputOrigin, AppOrigin: "http://localhost:8080", PortalOrigin: "http://localhost:3000", MaxJSONBytes: 65536}}).Routes()
	outputServer.Config.Handler = handler
	outputServer.Start()
	t.Cleanup(outputServer.Close)
	path := "/api/v1/tenants/" + tenant.String() + "/semantic-intents/" + intent.String() + "/targets"
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
	if w := get(path, ""); w.Code != 401 {
		t.Fatal("unauthenticated", w.Code)
	}
	capabilityResponse := get("/api/v1/tenants/"+tenant.String()+"/capabilities", token)
	if capabilityResponse.Code != 200 {
		t.Fatal("authorized capability projection", capabilityResponse.Code)
	}
	var deployment capability.Capabilities
	if json.Unmarshal(capabilityResponse.Body.Bytes(), &deployment) != nil {
		t.Fatal("invalid capability response")
	}
	own := map[string]bool{"semantic.preview_local_exposure": true, "semantic.preview_remove_object": true, "semantic.preview_background_cleanup": true, "semantic.adopt_shown_exposure": true, "semantic.adopt_shown_removal": true, "semantic.adopt_shown_background_cleanup": true}
	foundOwn := 0
	for _, operation := range deployment.Operations {
		if !own[operation.Operation] {
			continue
		}
		foundOwn++
		if operation.Available || operation.Status != "unqualified" || string(operation.AvailableBackends) != "[]" || operation.Reason == nil || *operation.Reason != "api_installation_missing" {
			t.Fatal("missing API installation advertised ready", operation)
		}
	}
	if foundOwn != len(own) {
		t.Fatal("own semantic registry rows missing", foundOwn)
	}
	reviewPath := "/api/v1/tenants/" + tenant.String() + "/semantic-intents/" + intent.String() + "/review"
	if response := get(reviewPath, ""); response.Code != 401 {
		t.Fatal("unauthenticated review projection", response.Code)
	}
	expectedKind, err := kindReader(ctx, auth.Identity{UserID: actor, SessionID: session}, tenant, intent)
	if err != nil {
		t.Fatal("verified intent fixture required", err)
	}
	reviewResponse := get(reviewPath, token)
	if reviewResponse.Code != 200 {
		t.Fatal("authorized review projection", reviewResponse.Code)
	}
	var reviewStatus semanticreview.ReviewStatus
	if json.Unmarshal(reviewResponse.Body.Bytes(), &reviewStatus) != nil || reviewStatus.Kind == nil || *reviewStatus.Kind != expectedKind {
		t.Fatal("verified review kind mismatch")
	}
	foreignReview := "/api/v1/tenants/" + uuid.NewString() + "/semantic-intents/" + intent.String() + "/review"
	if response := get(foreignReview, token); response.Code == 200 {
		t.Fatal("foreign tenant review kind exposed")
	}
	w := get(path+"?limit=1", token)
	outputPath := "/api/v1/tenants/" + tenant.String() + "/runs/" + child.String() + "/outputs"
	childStatusPath := strings.TrimSuffix(outputPath, "/outputs")
	childStatusResponse := get(childStatusPath, token)
	if childStatusResponse.Code != 200 {
		t.Fatal("child status unavailable", childStatusResponse.Code)
	}
	outputResponse := get(outputPath, token)
	if outputResponse.Code != 200 {
		t.Fatal("preview outputs unavailable", outputResponse.Code)
	}
	var outputPage struct {
		Items []runservice.Output `json:"items"`
	}
	if json.Unmarshal(outputResponse.Body.Bytes(), &outputPage) != nil || len(outputPage.Items) != 4 {
		t.Fatal("preview output projection mismatch")
	}
	images := 0
	var issuedDerivativeURL string
	for _, output := range outputPage.Items {
		if output.Role != "derivative" {
			continue
		}
		images++
		issuedDerivativeURL = output.URL
		request, err := http.NewRequestWithContext(ctx, "GET", output.URL, nil)
		if err != nil {
			t.Fatal("invalid signed output URL")
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal("signed output unavailable")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, output.Bytes+1))
		response.Body.Close()
		sum := sha256.Sum256(body)
		if readErr != nil || response.StatusCode != 200 || int64(len(body)) != output.Bytes || hex.EncodeToString(sum[:]) != output.SHA256 {
			t.Fatal("signed output integrity mismatch")
		}
	}
	if capture := os.Getenv("PHOTO_TEST_V27_OUTPUTS_RESPONSE_FILE"); capture != "" {
		body, err := json.Marshal(map[string]any{"tenant": tenant.String(), "run": child.String(), "response": json.RawMessage(outputResponse.Body.Bytes())})
		if err != nil {
			t.Fatal("output capture failed")
		}
		if err = os.WriteFile(capture, body, 0600); err != nil {
			t.Fatal("output capture write failed")
		}
	}
	if images != 2 {
		t.Fatal("expected two preview images")
	}
	if denied := get(outputPath, ""); denied.Code != 401 {
		t.Fatal("anonymous output accepted", denied.Code)
	}
	if w.Code != 200 {
		t.Fatal("completed target GET", w.Code, w.Body.String())
	}
	var page pagination.Page[semanticreview.TargetResource]
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.NextCursor != nil {
		t.Fatal("invalid page")
	}
	if output := os.Getenv("PHOTO_TEST_V27_TARGETS_RESPONSE_FILE"); output != "" {
		captured, err := json.Marshal(struct {
			Tenant   uuid.UUID       `json:"tenant"`
			Intent   uuid.UUID       `json:"intent"`
			Response json.RawMessage `json:"response"`
		}{tenant, intent, w.Body.Bytes()})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, captured, 0600); err != nil {
			t.Fatal(err)
		}
	}
	item := page.Items[0]
	if item.ID != target || item.Data.State != "applied" || item.Data.ChildRunID == nil || *item.Data.ChildRunID != child || item.Document == nil {
		t.Fatal("completed projection missing")
	}
	if w := get(path+"?limit=101", token); w.Code != 422 {
		t.Fatal("unbounded page", w.Code)
	}
	if w := get(path+"?cursor=tampered", token); w.Code != 400 {
		t.Fatal("invalid cursor", w.Code)
	}
	missing := "/api/v1/tenants/" + tenant.String() + "/semantic-intents/" + uuid.NewString() + "/targets"
	if w := get(missing, token); w.Code != 404 {
		t.Fatal("unknown intent", w.Code)
	}

	// Close through the real authenticated HTTP handler and object publisher.
	tx, err := api.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := semanticreview.LoadReviewIdentity(ctx, tx, tenant, intent, actor, session)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	sources, err := semanticreview.LoadPlanSources(ctx, tx, tenant, identity.WorkflowID, actor, session)
	tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var model, dataset string
	if err := owner.QueryRow(ctx, `SELECT config_snapshot->'v27_semantic'->'qualification'->>'model_lock_sha',config_snapshot->'v27_semantic'->'qualification'->>'dataset_sha' FROM photo.runs WHERE tenant_id=$1 AND id=$2`, tenant, identity.RunID).Scan(&model, &dataset); err != nil {
		t.Fatal(err)
	}
	planSHA := ""
	for _, source := range sources.Objects {
		if source.Contract == "SemanticPlan" {
			planSHA = source.Ref.SHA256
		}
	}
	read, err := semanticreview.NewPlanObjectReader(objects, bucket)
	if err != nil {
		t.Fatal(err)
	}
	prepareClose, err := semanticreview.NewClosePreparation(api, objects, read, bucket, 67108864, model, dataset)
	if err != nil {
		t.Fatal(err)
	}
	closeService := &semanticreview.CloseService{API: api, Prepare: prepareClose}
	closeHandler := (&Server{DB: api, Auth: service, SemanticReviewClose: closeService, Config: config.Config{Environment: "development", AppOrigin: "http://localhost:8080", PortalOrigin: "http://localhost:3000", MaxJSONBytes: 65536}}).Routes()
	closeBody, _ := json.Marshal(semanticreview.ReviewClose{ExpectedRevision: strconv.FormatInt(sources.Revision, 10), PlanSHA: planSHA})
	closeRequest := func(key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", strings.TrimSuffix(path, "/targets")+"/close-review", strings.NewReader(string(closeBody)))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Photo-Client", "native")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		out := httptest.NewRecorder()
		closeHandler.ServeHTTP(out, r)
		return out
	}
	statusPath := strings.TrimSuffix(path, "/targets") + "/review"
	if denied := get(statusPath, ""); denied.Code != 401 {
		t.Fatal("anonymous review state accepted", denied.Code)
	}
	beforeClose := get(statusPath, token)
	var status semanticreview.ReviewStatus
	if beforeClose.Code != 200 || json.Unmarshal(beforeClose.Body.Bytes(), &status) != nil || status.RunID != identity.RunID || status.WorkflowID != identity.WorkflowID || status.Revision != strconv.FormatInt(sources.Revision, 10) || status.State != "waiting_review" || !status.CanClose || status.Remaining != 0 || status.PlanSHA == nil || *status.PlanSHA != planSHA {
		t.Fatal("review fence unavailable", beforeClose.Code, beforeClose.Body.String())
	}
	// Publish once, then hold both authenticated preparations before the final
	// transactions. This exercises admission concurrency, not publisher races.
	var closeDTO semanticreview.ReviewClose
	if err := json.Unmarshal(closeBody, &closeDTO); err != nil {
		t.Fatal(err)
	}
	preparedClose, err := prepareClose(ctx, auth.Identity{UserID: actor, SessionID: session}, tenant, intent, closeDTO)
	if err != nil {
		t.Fatal("concurrent close preparation", err)
	}
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	closeService.Prepare = func(ctx context.Context, _ auth.Identity, _, _ uuid.UUID, _ semanticreview.ReviewClose) (semanticreview.PreparedClose, error) {
		ready <- struct{}{}
		select {
		case <-release:
			return preparedClose, nil
		case <-ctx.Done():
			return semanticreview.PreparedClose{}, ctx.Err()
		}
	}
	type closeOutcome struct {
		key      string
		response *httptest.ResponseRecorder
	}
	outcomes := make(chan closeOutcome, 2)
	sameKeyRace := os.Getenv("PHOTO_TEST_V27_CLOSE_RACE") == "same-key"
	keys := []string{"physical-close-review", "physical-close-race"}
	if sameKeyRace {
		keys[1] = keys[0]
	}
	for _, key := range keys {
		go func(key string) { outcomes <- closeOutcome{key, closeRequest(key)} }(key)
	}
	for n := 0; n < 2; n++ {
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			close(release)
			t.Fatal("concurrent requests did not reach admission barrier")
		}
	}
	close(release)
	var closed *httptest.ResponseRecorder
	closeKey := ""
	successes := 0
	for n := 0; n < 2; n++ {
		var outcome closeOutcome
		select {
		case outcome = <-outcomes:
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent close admission did not finish")
		}
		if outcome.response.Code == 200 {
			successes++
			if closed != nil {
				if !sameKeyRace {
					t.Fatal("both competing keys closed parent")
				}
				if closed.Body.String() != outcome.response.Body.String() {
					t.Fatal("same-key admission returned different responses")
				}
			}
			closed = outcome.response
			closeKey = outcome.key
		} else if outcome.response.Code != 409 {
			t.Fatal("concurrent close unexpected response", outcome.response.Code, outcome.response.Body.String())
		}
	}
	closeService.Prepare = prepareClose
	if sameKeyRace && successes != 2 {
		t.Fatal("same-key admission must succeed and replay", successes)
	}
	if closed == nil {
		t.Fatal("neither concurrent close succeeded")
	}

	if closed.Code != 200 {
		t.Fatal("physical close failed", closed.Code, closed.Body.String())
	}
	result, err := semanticreview.DecodeCloseResult(closed.Body.Bytes())
	if err != nil || result.RunID != identity.RunID || result.Revision != strconv.FormatInt(sources.Revision+1, 10) {
		t.Fatal("close response mismatch", err)
	}
	afterClose := get(statusPath, token)
	if afterClose.Code != 200 || json.Unmarshal(afterClose.Body.Bytes(), &status) != nil || status.State != "completed" || status.CanClose || status.Revision != result.Revision {
		t.Fatal("closed review fence mismatch", afterClose.Code, afterClose.Body.String())
	}
	if stale := closeRequest("physical-close-new-key"); stale.Code != 409 {
		t.Fatal("closed parent admitted a fresh close", stale.Code, stale.Body.String())
	}
	if missing := get(strings.TrimSuffix(statusPath, "/review")+"-missing/review", token); missing.Code != 404 {
		t.Fatal("invalid intent route accepted", missing.Code)
	}
	unknownStatus := "/api/v1/tenants/" + tenant.String() + "/semantic-intents/" + uuid.NewString() + "/review"
	if missing := get(unknownStatus, token); missing.Code != 404 {
		t.Fatal("unknown review status response", missing.Code, missing.Body.String())
	}
	replayed := closeRequest(closeKey)
	if replayed.Code != 200 || replayed.Body.String() != closed.Body.String() {
		t.Fatal("close replay failed", replayed.Code, replayed.Body.String())
	}
	var batches, events int
	var committedReceipts int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='SemanticIntent.CloseReview' AND response_status=200`, tenant, actor).Scan(&committedReceipts); err != nil {
		t.Fatal(err)
	}
	if committedReceipts != 1 {
		t.Fatal("concurrent closure duplicated committed receipt", committedReceipts)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM photo.v2_review_batches WHERE tenant_id=$1 AND workflow_id=$2 AND action='close'`, tenant, identity.WorkflowID).Scan(&batches); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v27.review.closed'`, tenant, identity.RunID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id IN(SELECT decisions_artifact_id FROM photo.v2_review_batches WHERE tenant_id=$1 AND workflow_id=$2 AND action='close')`, tenant, identity.WorkflowID); err != nil {
		t.Fatal(err)
	}
	expiredReplay := closeRequest(closeKey)
	if expiredReplay.Code != 200 || expiredReplay.Body.String() != closed.Body.String() {
		t.Fatal("expired close evidence broke replay", expiredReplay.Code, expiredReplay.Body.String())
	}
	if batches != 1 || events != 1 {
		t.Fatal("closure replay duplicated effects", batches, events)
	}
	if capture := os.Getenv("PHOTO_TEST_V27_TARGETS_RESPONSE_FILE"); capture != "" {
		raw, readErr := os.ReadFile(capture)
		var fixture map[string]json.RawMessage
		if readErr != nil || json.Unmarshal(raw, &fixture) != nil {
			t.Fatal("target capture unavailable")
		}
		fixture["review_status"] = append(json.RawMessage(nil), beforeClose.Body.Bytes()...)
		fixture["close_request"] = closeBody
		fixture["child_status"] = append(json.RawMessage(nil), childStatusResponse.Body.Bytes()...)
		fixture["close_response"] = append(json.RawMessage(nil), closed.Body.Bytes()...)
		encoded, encodeErr := json.Marshal(fixture)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if err := os.WriteFile(capture, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if sameKeyRace {
		t.Log("V27_CLOSE_SAME_KEY_OK")
	}
	t.Log("V27_CLOSE_HTTP_OK")
	// Additional unsupported rows exercise keyset ordering with identical times.
	// They are storage fixtures, not newly computed candidates.
	for n := 0; n < 2; n++ {
		var version uuid.UUID
		if err := owner.QueryRow(ctx, `INSERT INTO photo.asset_versions(tenant_id,asset_id,version_no,master_artifact_id,content_hash,metadata,review_state) SELECT tenant_id,asset_id,version_no+$3,master_artifact_id,content_hash,metadata,review_state FROM photo.asset_versions WHERE tenant_id=$1 AND id=$2 RETURNING id`, tenant, item.Data.AssetVersionID, n+1).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO photo.semantic_targets(tenant_id,project_id,intent_id,asset_version_id,state,created_at) VALUES($1,$2,$3,$4,'unsupported',$5)`, tenant, item.Data.ProjectID, intent, version, item.Data.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
	var expected []uuid.UUID
	rows, err := owner.Query(ctx, "SELECT id FROM photo.semantic_targets WHERE tenant_id=$1 AND intent_id=$2 ORDER BY created_at,id", tenant, intent)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		expected = append(expected, id)
	}
	rows.Close()
	var after string
	var seen []uuid.UUID
	for n := 0; n < 3; n++ {
		u := path + "?limit=1"
		if after != "" {
			u += "&cursor=" + url.QueryEscape(after)
		}
		w := get(u, token)
		if w.Code != 200 {
			t.Fatal("keyset page", w.Code)
		}
		var page pagination.Page[semanticreview.TargetResource]
		if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
			t.Fatal("invalid keyset page")
		}
		seen = append(seen, page.Items[0].ID)
		if n < 2 {
			if page.NextCursor == nil {
				t.Fatal("missing continuation")
			}
			after = *page.NextCursor
		} else if page.NextCursor != nil {
			t.Fatal("extra continuation")
		}
	}
	for n := range expected {
		if seen[n] != expected[n] {
			t.Fatal("keyset reordered or lost a target")
		}
	}
	viewerHandler, _, viewerToken, viewerTenant := httpFixture(t)
	var viewer uuid.UUID
	if err := owner.QueryRow(ctx, "SELECT owner_user_id FROM photo.tenants WHERE id=$1", viewerTenant).Scan(&viewer); err != nil {
		t.Fatal(err)
	}
	if w := request(viewerHandler, "GET", path, "", viewerToken, "", ""); w.Code != 404 {
		t.Fatal("foreign tenant exposed", w.Code)
	}
	var viewerSession uuid.UUID
	var viewerEpoch int64
	if err := owner.QueryRow(ctx, "SELECT s.id,u.auth_epoch FROM photo.sessions s JOIN photo.users u ON u.id=s.user_id WHERE s.user_id=$1 AND s.revoked_at IS NULL ORDER BY s.created_at DESC LIMIT 1", viewer).Scan(&viewerSession, &viewerEpoch); err != nil {
		t.Fatal("viewer session missing", err)
	}
	previewViewerToken, err := service.Signer.Issue(viewer, viewerSession, viewerEpoch)
	if err != nil {
		t.Fatal("viewer token unavailable")
	}
	if denied := get(childStatusPath, previewViewerToken); denied.Code != 404 {
		t.Fatal("unauthorized child progress exposed", denied.Code)
	}
	if denied := get(outputPath, previewViewerToken); denied.Code != 404 {
		t.Fatal("foreign preview tenant exposed", denied.Code)
	}
	if _, err := owner.Exec(ctx, "INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'viewer','active')", tenant, viewer); err != nil {
		t.Fatal(err)
	}
	project := item.Data.ProjectID
	if _, err := owner.Exec(ctx, "UPDATE photo.projects SET visibility='restricted' WHERE tenant_id=$1 AND id=$2", tenant, project); err != nil {
		t.Fatal(err)
	}
	if w := request(viewerHandler, "GET", path, "", viewerToken, "", ""); w.Code != 404 {
		t.Fatal("restricted project exposed", w.Code)
	}
	if denied := get(childStatusPath, previewViewerToken); denied.Code != 404 {
		t.Fatal("unauthorized child progress exposed", denied.Code)
	}
	if denied := get(outputPath, previewViewerToken); denied.Code != 404 {
		t.Fatal("restricted preview exposed", denied.Code)
	}
	if _, err := owner.Exec(ctx, "INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')", tenant, project, viewer); err != nil {
		t.Fatal(err)
	}
	if w := request(viewerHandler, "GET", path, "", viewerToken, "", ""); w.Code != 200 {
		t.Fatal("viewer denied", w.Code, w.Body.String())
	}
	if denied := get(statusPath, previewViewerToken); denied.Code != 404 {
		t.Fatal("viewer received editor review authority", denied.Code, denied.Body.String())
	}
	if allowed := get(childStatusPath, previewViewerToken); allowed.Code != 200 {
		t.Fatal("authorized child progress denied", allowed.Code)
	}
	allowedPreview := get(outputPath, previewViewerToken)
	var viewerOutputs struct {
		Items []runservice.Output `json:"items"`
	}
	if allowedPreview.Code != 200 || json.Unmarshal(allowedPreview.Body.Bytes(), &viewerOutputs) != nil || len(viewerOutputs.Items) != 4 {
		t.Fatal("preview viewer denied", allowedPreview.Code)
	}
	viewerPage := request(viewerHandler, "GET", path+"?limit=1", "", viewerToken, "", "")
	var viewerListing pagination.Page[semanticreview.TargetResource]
	if json.Unmarshal(viewerPage.Body.Bytes(), &viewerListing) != nil || viewerListing.NextCursor == nil {
		t.Fatal("viewer continuation missing")
	}
	viewerContinuation := path + "?limit=1&cursor=" + url.QueryEscape(*viewerListing.NextCursor)
	if _, err := owner.Exec(ctx, "DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3", tenant, project, viewer); err != nil {
		t.Fatal(err)
	}
	if w := request(viewerHandler, "GET", path, "", viewerToken, "", ""); w.Code != 404 {
		t.Fatal("revoked ACL ignored", w.Code)
	}
	if denied := get(childStatusPath, previewViewerToken); denied.Code != 404 {
		t.Fatal("unauthorized child progress exposed", denied.Code)
	}
	if denied := get(outputPath, previewViewerToken); denied.Code != 404 {
		t.Fatal("preview ACL revocation ignored", denied.Code)
	}
	if w := request(viewerHandler, "GET", viewerContinuation, "", viewerToken, "", ""); w.Code != 404 {
		t.Fatal("cursor bypassed revoked ACL", w.Code)
	}

	if _, err := owner.Exec(ctx, "UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2", tenant, project); err != nil {
		t.Fatal(err)
	}
	if w := get(path, token); w.Code != 404 {
		t.Fatal("archived project exposed", w.Code)
	}
	if denied := get(statusPath, token); denied.Code != 404 {
		t.Fatal("archived review exposed", denied.Code, denied.Body.String())
	}
	if denied := get(childStatusPath, token); denied.Code != 404 {
		t.Fatal("archived child progress exposed", denied.Code)
	}
	if denied := get(outputPath, token); denied.Code != 404 {
		t.Fatal("archived preview exposed", denied.Code)
	}
	// A previously issued capability must still check current project authority.
	archivedRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, issuedDerivativeURL, nil)
	if err != nil {
		t.Fatal("archived capability request", err)
	}
	archivedMedia, err := http.DefaultClient.Do(archivedRequest)
	if err != nil {
		t.Fatal("archived capability request", err)
	}
	archivedMedia.Body.Close()
	if archivedMedia.StatusCode != http.StatusNotFound {
		t.Fatal("issued capability bypassed archive", archivedMedia.StatusCode)
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.projects SET archived_at=NULL WHERE tenant_id=$1 AND id=$2", tenant, project); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE user_id=$1", viewer); err != nil {
		t.Fatal(err)
	}
	if w := request(viewerHandler, "GET", path, "", viewerToken, "", ""); w.Code != 401 {
		t.Fatal("revoked session accepted", w.Code)
	}
	if denied := get(childStatusPath, previewViewerToken); denied.Code != 401 {
		t.Fatal("revoked session exposed child progress", denied.Code)
	}
	if denied := get(outputPath, previewViewerToken); denied.Code != 401 {
		t.Fatal("revoked preview session accepted", denied.Code)
	}
	t.Log("V27_TARGETS_HTTP_OK")
}
