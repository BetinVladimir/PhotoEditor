package semanticproposal

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgtype"
	"io"
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/masknpy"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"

	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"photoeditor/backend/internal/v2input"
	"strings"
	"sync"
)

// Source-only candidate: actual HTTP/auth and native initial preparation.
// No accepted parent or producer result is seeded by this helper.
func probeSceneCreateHTTP(t *testing.T, ctx context.Context, service CreateHTTPProposer, preparer TransferDraftPreparer, owner *pgxpool.Pool, who auth.Identity, request Request, key string, uncovered bool) {
	t.Helper()
	reader := preparer.Reader
	ordinaryRecovery := SQLNativeInitialPipelineRecovery{Preparer: preparer, CatalogForReference: (SQLNativeParentArtifactCatalogFactory{API: preparer.Reader.DB, Bucket: preparer.Reader.Bucket}).ForReference, Timeout: 2 * time.Minute}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("create HTTP signing key", err)
	}
	poolConfig := owner.Config().Copy()
	poolConfig.ConnConfig.RuntimeParams["role"] = "photo_auth"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("create HTTP auth pool unavailable")
	}
	t.Cleanup(pool.Close)
	authService := &auth.Service{DB: pool, Pepper: make([]byte, 32), Signer: auth.TokenSigner{Active: "fixture", Private: private, Public: map[string]ed25519.PublicKey{"fixture": public}, Issuer: "fixture", Audience: "fixture"}}
	var epoch int64
	if err := owner.QueryRow(ctx, "SELECT auth_epoch FROM photo.users WHERE id=$1", who.UserID).Scan(&epoch); err != nil {
		t.Fatal("create HTTP actor unavailable")
	}
	token, err := authService.Signer.Issue(who.UserID, who.SessionID, epoch)
	if err != nil {
		t.Fatal("create HTTP access token unavailable")
	}
	if CreateHTTPHandler == nil {
		t.Fatal("external HTTP harness not installed")
	}
	server := httptest.NewServer(CreateHTTPHandler(reader.DB, authService, service))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 30 * time.Second
	body, err := json.Marshal(request.Intent)
	if err != nil {
		t.Fatal(err)
	}
	post := func(tenant uuid.UUID, access, key string, raw []byte, web bool) (int, []byte) {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/tenants/"+tenant.String()+"/semantic-intents", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		if web {
			r.Header.Set("Origin", "http://localhost:8080")
		} else {
			r.Header.Set("X-Photo-Client", "native")
		}
		if access != "" {
			r.Header.Set("Authorization", "Bearer "+access)
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal("create HTTP request failed", err)
		}
		defer response.Body.Close()
		result, err := io.ReadAll(io.LimitReader(response.Body, 65537))
		if err != nil || len(result) > 65536 {
			t.Fatal("create HTTP response invalid")
		}
		return response.StatusCode, result
	}
	for _, negative := range []struct {
		access, key string
		body        []byte
		web         bool
		status      int
	}{
		{"", key, body, false, 401},
		{token, key, body, true, 403},
		{token, "", body, false, 400},
		{token, key, []byte(`{"unknown":true}`), false, 422},
	} {
		if status, _ := post(request.TenantID, negative.access, negative.key, negative.body, negative.web); status != negative.status {
			t.Fatal("create HTTP guard", status, negative.status)
		}
	}
	if status, _ := post(uuid.New(), token, key, body, false); status < 400 || status >= 500 {
		t.Fatal("create HTTP foreign tenant guard", status)
	}

	if preparer.Configuration.Scene == nil {
		t.Fatal("scene HTTP fixture missing immutable scene policy")
	}
	assertNoParent := func() {
		t.Helper()
		var effects int
		if err := owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2)+(SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND resource_id=$2 AND route='V2.Admit')+(SELECT count(*) FROM photo.v2_workflows WHERE tenant_id=$1 AND run_id=$2)+(SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2)+(SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$2::text)+(SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2)`, request.TenantID, request.RunID).Scan(&effects); err != nil || effects != 0 {
			t.Fatal("private scene preparation emitted public parent", effects, err)
		}
	}
	status, raw := post(request.TenantID, token, key, body, false)
	if status != 503 || !bytes.Contains(raw, []byte("DEPENDENCY_UNAVAILABLE")) {
		t.Fatal("scene HTTP pending preparation", status)
	}
	assertNoParent()
	presence, presenceErr := reader.readInitialNativeDetectorPresence(ctx, who, request, key)
	if presenceErr != nil || presence != nativeStagePresent {
		t.Fatal("scene HTTP pending checkpoint missing", presence, presenceErr)
	}
	t.Logf("V27_SCENE_PUBLIC_PARENT_ID=%s", request.RunID)
	personSucceeded := false
	var accepted []byte
	for step := 0; step < 256; step++ {
		status, raw = post(request.TenantID, token, key, body, false)
		if status == 202 {
			accepted = append([]byte(nil), raw...)
			break
		}
		if status != 503 || !bytes.Contains(raw, []byte("DEPENDENCY_UNAVAILABLE")) {
			t.Fatal("scene HTTP preparation retry", status)
		}
		assertNoParent()
		recoveryStarted := time.Now()
		apiBefore, schedulerBefore := preparer.Reader.DB.Stat(), preparer.Scheduler.Stat()
		_, locked, recoverErr := ordinaryRecovery.Recover(ctx, who, request.TenantID, request.RunID)
		if recoverErr != nil || !locked {
			for index, pool := range []*pgxpool.Pool{preparer.Reader.DB, preparer.Scheduler} {
				before := apiBefore
				role := "api"
				if index == 1 {
					before, role = schedulerBefore, "scheduler"
				}
				after := pool.Stat()
				t.Logf("NATIVE_RECOVERY_TIMING step=%d elapsed=%s pool=%s acquired=%d idle=%d total=%d acquire_count_delta=%d acquire_duration_delta=%s canceled_acquire_delta=%d empty_acquire_delta=%d", step, time.Since(recoveryStarted), role, after.AcquiredConns(), after.IdleConns(), after.TotalConns(), after.AcquireCount()-before.AcquireCount(), after.AcquireDuration()-before.AcquireDuration(), after.CanceledAcquireCount()-before.CanceledAcquireCount(), after.EmptyAcquireCount()-before.EmptyAcquireCount())
			}
		}
		if uncovered && errors.Is(recoverErr, masknpy.ErrInvalid) {
			if !personSucceeded {
				t.Fatal("uncovered refusal preceded actual person canonical output")
			}
			assertNoParent()
			if code, response := post(request.TenantID, token, key, body, false); code != 503 || !bytes.Contains(response, []byte("DEPENDENCY_UNAVAILABLE")) {
				t.Fatal("uncovered scene HTTP parent admitted", code)
			}
			t.Log("V27_SCENE_CREATE_UNCOVERED_PARENT_REFUSED_OK")
			return
		}
		if recoverErr != nil || !locked {
			t.Fatal("scene HTTP durable preparation", locked, recoverErr)
		}
		rows, err := owner.Query(ctx, `SELECT t.id,t.operation,t.run_id FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE r.tenant_id=$1 AND r.id<>$2 AND t.state='ready' AND jsonb_path_exists(r.config_snapshot,'$.*.parent_run_id ? (@ == $parent)',jsonb_build_object('parent',$2::text)) ORDER BY t.id`, request.TenantID, request.RunID)
		if err != nil {
			t.Fatal(err)
		}
		var tasks []struct {
			ID        uuid.UUID
			Operation string
			Run       uuid.UUID
		}
		for rows.Next() {
			var task struct {
				ID        uuid.UUID
				Operation string
				Run       uuid.UUID
			}
			if err = rows.Scan(&task.ID, &task.Operation, &task.Run); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			tasks = append(tasks, task)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range tasks {
			if !scenePreparationWorkerScope(request.RunID, task.Run, task.Operation) {
				t.Fatal("scene preparation worker escaped prerequisite scope")
			}
			probeNativeWireTaskOutputs(t, ctx, task.ID)
			if task.Operation == "mask.person" {
				var ready bool
				if err := owner.QueryRow(ctx, `SELECT t.state='succeeded' AND r.task_id=t.id AND r.attempt=t.attempt AND r.fencing_token=t.fencing_token AND jsonb_array_length(r.outputs)=4 FROM photo.tasks t JOIN photo.task_results r ON r.tenant_id=t.tenant_id AND r.id=t.canonical_result_id WHERE t.tenant_id=$1 AND t.id=$2`, request.TenantID, task.ID).Scan(&ready); err != nil || !ready {
					t.Fatal("scene person canonical output missing", err)
				}
				personSucceeded = true
				t.Logf("V27_SCENE_PUBLIC_PERSON_TASK_ID=%s", task.ID)
			}
		}
	}
	if uncovered {
		t.Fatal("uncovered scene fixture reached accepted parent")
	}
	if !personSucceeded {
		t.Fatal("scene public create did not execute its person producer")
	}
	t.Log("V27_SCENE_CREATE_MEASURED_PERSON_OUTPUTS_OK")
	// Preparation may admit the parent during recovery, but this worker must
	// leave every parent task untouched for the executable's pending check.
	var parentQueued bool
	if err := owner.QueryRow(ctx, `SELECT r.status='queued' AND r.revision=2 AND r.graph_sealed
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id
 AND (t.attempt<>0 OR t.canonical_result_id IS NOT NULL OR t.state NOT IN('ready','blocked')))
 FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$2`, request.TenantID, request.RunID).Scan(&parentQueued); err != nil || !parentQueued {
		t.Fatal("scene preparation consumed public parent before pending executable check")
	}
	var ticket v2input.JobTicket
	if len(accepted) == 0 || json.Unmarshal(accepted, &ticket) != nil || ticket.RunID != request.RunID || ticket.WorkflowID != request.WorkflowID || ticket.State != "queued" {
		t.Fatal("scene HTTP accepted receipt missing")
	}
	for retry := 0; retry < 2; retry++ {
		if code, reply := post(request.TenantID, token, key, body, false); code != 202 || !bytes.Equal(reply, accepted) {
			t.Fatal("scene HTTP exact replay", code)
		}
	}
	changed := request.Intent
	changed.Strength = .91
	if changed.Strength == request.Intent.Strength {
		changed.Strength = .81
	}
	if changed.Strength == request.Intent.Strength {
		t.Fatal("changed request strength unchanged")
	}
	changedBody, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := post(request.TenantID, token, key, changedBody, false); code != 409 {
		t.Fatal("scene HTTP changed request conflict", code)
	}
	var events, messages, receipts int
	if err = owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'),(SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$2::text AND routing_key='v2.run.queued'),(SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND resource_id=$2 AND route='V2.Admit' AND response_status=202)`, request.TenantID, request.RunID).Scan(&events, &messages, &receipts); err != nil || events != 1 || messages != 1 || receipts != 1 {
		t.Fatal("scene HTTP duplicate effects", events, messages, receipts, err)
	}
	var priorRevocation pgtype.Timestamptz
	if err = owner.QueryRow(ctx, "SELECT revoked_at FROM photo.sessions WHERE id=$1", who.SessionID).Scan(&priorRevocation); err != nil || priorRevocation.Valid {
		t.Fatal("scene session must initially be live", err)
	}
	if _, err = owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=$2 WHERE id=$1", who.SessionID, priorRevocation); err != nil {
				t.Error("scene HTTP restore session", err)
			}
		}()
		if code, _ := post(request.TenantID, token, key, body, false); code != 401 {
			t.Fatal("scene HTTP revoked replay", code)
		}
	}()
	// Executable startup integration currently binds the original public key.
	// Caller must use that key, never relabel a different parent's replay.
	if key != "proposal-publication" {
		t.Fatal("scene startup fixture key mismatch")
	}
	createDeploymentForProbe(t, preparer)
	startup := exec.CommandContext(ctx, "go", "test", "./cmd/photo", "-json", "-run", "^TestIntegrationSemanticCreateDeployment$", "-count=1", "-timeout=90s", "-v")
	startup.Dir = "../.."
	var capture sceneStartupCapture
	startup.Stdout = &capture
	startup.Stderr = &capture
	err = startup.Run()
	output, overflow := capture.snapshot()
	root, rootErr := filepath.Abs("../../..")
	if rootErr != nil {
		t.Fatal("scene startup diagnostic root unavailable")
	}
	saveSceneStartupDiagnostic(t, root, output, overflow, err)
	if !sceneStartupPassed(output, overflow, err) || !strings.Contains(string(output), "V27_CREATE_DEPLOYMENT_BUILD_OK") || !strings.Contains(string(output), "V27_CREATE_EXECUTABLE_REPLAY_SHUTDOWN_OK") {
		t.Fatal("scene create executable replay failed; bounded private diagnostic retained")
	}
	t.Log("V27_SCENE_CREATE_HTTP_AUTH_ADMISSION_REPLAY_OK")
	t.Log("V27_SCENE_CREATE_EXECUTABLE_REPLAY_SHUTDOWN_OK")
}

// Private bounded child transcript; arbitrary Output never enters public logs.
const sceneStartupLimit = 4 << 20
const sceneStartupPackage = "photoeditor/backend/cmd/photo"
const sceneStartupRoot = "TestIntegrationSemanticCreateDeployment"

type sceneStartupCapture struct {
	mu       sync.Mutex
	body     bytes.Buffer
	overflow bool
}

func (b *sceneStartupCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := sceneStartupLimit - b.body.Len()
	if n > left {
		b.overflow = true
		p = p[:left]
	}
	_, err := b.body.Write(p)
	return n, err
}
func (b *sceneStartupCapture) snapshot() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.body.Bytes()...), b.overflow
}

type sceneStartupEvent struct {
	Action string `json:"action"`
	Test   string `json:"test,omitempty"`
}
type sceneStartupSummary struct {
	Package    string              `json:"package"`
	Events     []sceneStartupEvent `json:"events"`
	Malformed  int                 `json:"malformed"`
	Foreign    int                 `json:"foreign"`
	Overflow   bool                `json:"overflow"`
	ExitFailed bool                `json:"exit_failed"`
	Bytes      int                 `json:"bytes"`
}

func summarizeSceneStartup(raw []byte, overflow bool, exitErr error) sceneStartupSummary {
	out := sceneStartupSummary{Package: sceneStartupPackage, Events: []sceneStartupEvent{}, Overflow: overflow || len(raw) > sceneStartupLimit, ExitFailed: exitErr != nil, Bytes: len(raw)}
	scan := raw
	if len(scan) > sceneStartupLimit {
		scan = scan[:sceneStartupLimit]
	}
	for _, line := range strings.Split(strings.TrimSpace(string(scan)), "\n") {
		var e struct{ Action, Package, Test string }
		if json.Unmarshal([]byte(line), &e) != nil {
			out.Malformed++
			continue
		}
		if e.Package != sceneStartupPackage || (e.Test != "" && e.Test != sceneStartupRoot) {
			out.Foreign++
			continue
		}
		switch e.Action {
		case "run", "pass", "fail", "skip":
			if len(out.Events) < 32 {
				out.Events = append(out.Events, sceneStartupEvent{e.Action, e.Test})
			} else {
				out.Overflow = true
			}
		}
	}
	return out
}
func sceneStartupPassed(raw []byte, overflow bool, exitErr error) bool {
	s := summarizeSceneStartup(raw, overflow, exitErr)
	if s.Overflow || s.ExitFailed || s.Malformed != 0 || s.Foreign != 0 {
		return false
	}
	run, pass, pkg := 0, 0, 0
	for _, e := range s.Events {
		if e.Action == "fail" || e.Action == "skip" {
			return false
		}
		if e.Test == sceneStartupRoot {
			if e.Action == "run" {
				run++
				if pass > 0 {
					return false
				}
			}
			if e.Action == "pass" {
				pass++
				if run != 1 {
					return false
				}
			}
		} else if e.Action == "pass" {
			pkg++
		}
	}
	return run == 1 && pass == 1 && pkg == 1
}
func saveSceneStartupDiagnostic(t *testing.T, root string, raw []byte, overflow bool, exitErr error) {
	t.Helper()
	if len(raw) > sceneStartupLimit {
		t.Fatal("scene startup diagnostic bounds invalid")
	}
	dir, err := os.MkdirTemp(filepath.Join(root, "work"), "scene-create-startup-private-")
	if err != nil {
		t.Fatal("scene startup diagnostic directory unavailable")
	}
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("scene startup diagnostic directory permission")
	}
	f, err := os.OpenFile(filepath.Join(dir, "stdout.private.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("scene startup diagnostic file unavailable")
	}
	_, we := f.Write(raw)
	se := f.Sync()
	ce := f.Close()
	if we != nil || se != nil || ce != nil {
		t.Fatal("scene startup diagnostic persistence failed")
	}
	summary := summarizeSceneStartup(raw, overflow, exitErr)
	safe, err := json.Marshal(summary)
	if err != nil {
		t.Fatal("scene startup summary invalid")
	}
	if os.WriteFile(filepath.Join(dir, "summary.json"), safe, 0600) != nil {
		t.Fatal("scene startup summary persistence failed")
	}
	t.Logf("SCENE_CREATE_STARTUP_DIAGNOSTIC private_dir=%s sha256=%x summary=%s", dir, sha256.Sum256(raw), safe)
}
func TestSceneCreateStartupDiagnostic(t *testing.T) {
	event := func(a, p, n string) string {
		b, e := json.Marshal(map[string]string{"Action": a, "Package": p, "Test": n, "Output": "PRIVATE_SENTINEL"})
		if e != nil {
			t.Fatal(e)
		}
		return string(b) + "\n"
	}
	run := event("run", sceneStartupPackage, sceneStartupRoot)
	pass := event("pass", sceneStartupPackage, sceneStartupRoot)
	pkg := event("pass", sceneStartupPackage, "")
	base := run + pass + pkg
	if !sceneStartupPassed([]byte(base), false, nil) {
		t.Fatal("genuine-shaped baseline")
	}
	rows := []string{pass + run + pkg, run + pkg, base + run, base + event("skip", sceneStartupPackage, sceneStartupRoot), base + event("fail", sceneStartupPackage, sceneStartupRoot), base + "not-json\n", run + event("pass", "foreign", sceneStartupRoot) + pkg}
	for _, raw := range rows {
		if !sceneStartupPassed([]byte(base), false, nil) {
			t.Fatal("positive baseline")
		}
		if sceneStartupPassed([]byte(raw), false, nil) {
			t.Fatal("invalid transcript accepted")
		}
	}
	if sceneStartupPassed([]byte(base), true, nil) || sceneStartupPassed([]byte(base), false, context.Canceled) {
		t.Fatal("overflow/nonzero accepted")
	}
	safe, _ := json.Marshal(summarizeSceneStartup([]byte(base), false, nil))
	if strings.Contains(string(safe), "PRIVATE_SENTINEL") {
		t.Fatal("private output escaped")
	}
	var capture sceneStartupCapture
	raw := bytes.Repeat([]byte("x"), sceneStartupLimit+1)
	if n, e := capture.Write(raw); e != nil || n != len(raw) {
		t.Fatal("capture drain")
	}
	body, overflow := capture.snapshot()
	if !overflow || len(body) != sceneStartupLimit {
		t.Fatal("capture bounds")
	}
}

// Only prerequisite child runs may be driven by the preparation worker.
// The parent's frozen transfer marker also contains its own parent_run_id.
func scenePreparationWorkerScope(parent, run uuid.UUID, operation string) bool {
	if parent == uuid.Nil || run == uuid.Nil || run == parent {
		return false
	}
	switch operation {
	case "face.detect", "mask.person", "face.landmarks", "face.parse", "mask.face_skin", "defects.propose", "scene.parse", "depth.predict":
		return true
	default:
		return false
	}
}

func TestScenePreparationWorkerScope(t *testing.T) {
	parent, child := uuid.New(), uuid.New()
	for _, operation := range []string{"face.detect", "mask.person", "face.landmarks", "face.parse", "mask.face_skin", "defects.propose", "scene.parse", "depth.predict"} {
		if !scenePreparationWorkerScope(parent, child, operation) {
			t.Fatal("prerequisite child denied")
		}
		if scenePreparationWorkerScope(parent, parent, operation) {
			t.Fatal("self-parent marker granted preparation authority")
		}
	}
	for _, operation := range []string{"semantic.transfer", "v2.semantic.assemble", "", "unknown"} {
		if scenePreparationWorkerScope(parent, child, operation) || scenePreparationWorkerScope(parent, parent, operation) {
			t.Fatal("public or unknown task accepted as preparation")
		}
	}
	if scenePreparationWorkerScope(uuid.Nil, child, "scene.parse") || scenePreparationWorkerScope(parent, uuid.Nil, "scene.parse") {
		t.Fatal("empty run scope accepted")
	}
}
