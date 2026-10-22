package semanticproposal

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Actual saved child, reservation/claim, Python adapter, mTLS and immutable
// uploads. The nested probe explicitly uses a deterministic model test double.
func probeCreatePersonNewOutputs(t *testing.T, ctx context.Context, owner *pgxpool.Pool, tenant, parent uuid.UUID) {
	t.Helper()
	run, _, err := personPreparationIDs(parent)
	if err != nil {
		t.Fatal(err)
	}
	var task uuid.UUID
	if err := owner.QueryRow(ctx, "SELECT id FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND operation='mask.person' AND state='ready'", tenant, run).Scan(&task); err != nil {
		t.Fatal("native person ready child", err)
	}
	probeNativeWireTaskOutputs(t, ctx, task)
	t.Log("V27_CREATE_PERSON_NEW_OUTPUTS_COMMITTED_OK")
}

func probeNativeWireTaskOutputs(t *testing.T, ctx context.Context, task uuid.UUID) {
	t.Helper()
	var fixture struct {
		Objects struct{ Endpoint, Bucket, AccessKey, SecretKey string }
	}
	if err := json.Unmarshal([]byte(os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")), &fixture); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "go", "test", "./internal/gateway", "-run", "^TestIntegrationV27IndexedDownloads$", "-count=1", "-timeout=90s", "-json")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "PHOTO_TEST_V27_GATEWAY_TASK="+task.String(), "PHOTO_TEST_V27_REPO_ROOT="+root,
		"PHOTO_TEST_MINIO_URL="+fixture.Objects.Endpoint, "PHOTO_TEST_MINIO_BUCKET="+fixture.Objects.Bucket,
		"PHOTO_TEST_MINIO_ACCESS="+fixture.Objects.AccessKey, "PHOTO_TEST_MINIO_SECRET="+fixture.Objects.SecretKey)
	output, err := cmd.CombinedOutput()
	// Query the genuine owned task before accepting nested claim witnesses.
	witnessDB, witnessErr := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if witnessErr != nil { t.Fatal("nested witness database", witnessErr) }
	var witnessOperation string
	witnessErr = witnessDB.QueryRow(ctx, "SELECT operation FROM photo.tasks WHERE id=$1", task).Scan(&witnessOperation)
	witnessDB.Close()
	if witnessErr != nil { t.Fatal("nested witness task", witnessErr) }
	if witnessOperation == "semantic.transfer" {
        saveSemanticClaimDiagnostic(t, root, task.String(), output, err)
		if !semanticClaimNestedSucceeded(output, err) { t.Fatal("required semantic claim nested witnesses missing/invalid") }
		for _, name := range semanticClaimNestedNames { t.Log("SEMANTIC_CLAIM_NESTED_PASS " + name) }
	}
	if os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1" && !strings.Contains(string(output), "V27_WORKER_FAILURE_COMMITTED_OK") {
		t.Fatal("worker failure branch not executed", string(output))
	}
	if os.Getenv("PHOTO_TEST_V27_TARGET_WORKER_RETRY_ONCE") == "1" && os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1" {
		if !strings.Contains(string(output), "V27_TARGET_WORKER_RETRY_ONCE_OK") {
			t.Fatal("retry branch not executed", string(output))
		}
		t.Log("V27_TARGET_WORKER_RETRY_ONCE_OK")
	}
	if os.Getenv("PHOTO_TEST_V27_TARGET_WORKER_RETRY_EXHAUST") == "1" && os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1" {
		if !strings.Contains(string(output), "V27_TARGET_WORKER_RETRY_EXHAUST_OK") {
			t.Fatal("budget branch not executed", string(output))
		}
		t.Log("V27_TARGET_WORKER_RETRY_EXHAUST_OK")
	}
	if os.Getenv("PHOTO_TEST_V27_FAILURE_CODE") == "RESOURCE_OOM" && os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1" {
		if !strings.Contains(string(output), "V27_TARGET_WORKER_OOM_REFUSED_OK") {
			t.Fatal("OOM refusal branch not executed", string(output))
		}
		t.Log("V27_TARGET_WORKER_OOM_REFUSED_OK")
	}
	if strings.Contains(string(output), "V27_TARGET_WORKER_RETRY_SUCCESS_OK") {
		t.Log("V27_TARGET_WORKER_RETRY_SUCCESS_OK")
	}
	if err != nil || !strings.Contains(string(output), "V27_WORKER_WIRE_OK") {
		t.Fatal("new native person wire output", err, string(output))
	}
}

// Package/Test identity and exact run/pass pairs come from go test -json.
// Output is never forwarded as evidence; only the closed validated names are logged.
var semanticClaimNestedNames = []string{"expired-slot", "task-cancel", "run-cancel", "worker-disabled", "worker-unqualified", "wrong-attempt", "wrong-worker"}
func semanticClaimNestedJSON(raw []byte) bool {
 const pkg = "photoeditor/backend/internal/gateway"
 const root = "TestIntegrationV27IndexedDownloads"
 want := map[string][2]int{root: {0,0}}
 for _, name := range semanticClaimNestedNames { want[root+"/semantic-transfer-claim/"+name] = [2]int{} }
 packagePass := 0
 for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
  var event struct {Action, Package, Test string}
  if json.Unmarshal([]byte(line), &event) != nil { return false }
  if event.Package != pkg || event.Action == "fail" || event.Action == "skip" { return false }
  if event.Test == "" && event.Action == "pass" { packagePass++ }
  if count, ok := want[event.Test]; ok {
   if event.Action == "run" { count[0]++ }
   if event.Action == "pass" { if count[0]!=1 { return false }; count[1]++ }
   want[event.Test] = count
  }
 }
 if packagePass != 1 { return false }
 for _, count := range want { if count != [2]int{1,1} { return false } }
 return true
}

func semanticClaimNestedSucceeded(raw []byte, exitErr error) bool {
 return exitErr == nil && semanticClaimNestedJSON(raw)
}

func TestSemanticClaimNestedJSON(t *testing.T) {
 const pkg = "photoeditor/backend/internal/gateway"
 const root = "TestIntegrationV27IndexedDownloads"
 type event struct { Action, Package, Test string }
 positive := []event{{"run",pkg,root}}
 for _, name := range semanticClaimNestedNames {
  positive=append(positive,event{"run",pkg,root+"/semantic-transfer-claim/"+name},event{"pass",pkg,root+"/semantic-transfer-claim/"+name})
 }
 positive=append(positive,event{"pass",pkg,root},event{"pass",pkg,""})
 encode:=func(events []event) []byte { var lines []string;for _,e:=range events {raw,err:=json.Marshal(e);if err!=nil {t.Fatal(err)};lines=append(lines,string(raw))};return []byte(strings.Join(lines,"\n")+"\n") }
 base:=encode(positive)
 cases:=[]struct{name string;raw []byte;exitErr error;want bool}{{"positive",base,nil,true},{"nonzero-exit",base,context.Canceled,false},{"malformed",[]byte("{broken"),nil,false},{"trailing-invalid",append(append([]byte{},base...),[]byte("not-json\n")...),nil,false}}
 changed:=func() []event {return append([]event{},positive...)}
 rows:=changed();rows[1].Package="foreign";cases=append(cases,struct{name string;raw []byte;exitErr error;want bool}{"wrong-package",encode(rows),nil,false})
 rows=changed();rows=append(rows[:2],rows[3:]...);cases=append(cases,struct{name string;raw []byte;exitErr error;want bool}{"missing-pass",encode(rows),nil,false})
 rows=changed();rows=append(rows,rows[1]);cases=append(cases,struct{name string;raw []byte;exitErr error;want bool}{"duplicate-run",encode(rows),nil,false})
 rows=changed();rows[1],rows[2]=rows[2],rows[1];cases=append(cases,struct{name string;raw []byte;exitErr error;want bool}{"reversed-run-pass",encode(rows),nil,false})
 for _,action:=range []string{"skip","fail"} {rows=changed();rows=append(rows,event{action,pkg,root});cases=append(cases,struct{name string;raw []byte;exitErr error;want bool}{action,encode(rows),nil,false})}
 for _,c:=range cases {t.Run(c.name,func(t *testing.T){if !semanticClaimNestedSucceeded(base,nil){t.Fatal("positive baseline")};if got:=semanticClaimNestedSucceeded(c.raw,c.exitErr);got!=c.want {t.Fatalf("parser result got=%t want=%t",got,c.want)}})}
}


// Closed public metadata; arbitrary Output and foreign identities remain private.
type semanticClaimDiagnosticEvent struct { Action string `json:"action"`; Test string `json:"test,omitempty"` }
type semanticClaimDiagnosticSummary struct {
 Bytes int `json:"bytes"`; Truncated bool `json:"truncated"`; ExitFailed bool `json:"exit_failed"`
 Malformed int `json:"malformed"`; Foreign int `json:"foreign"`; Events []semanticClaimDiagnosticEvent `json:"events"`
}
func semanticClaimDiagnosticSummaryFor(raw []byte, exitErr error) semanticClaimDiagnosticSummary {
 s:=semanticClaimDiagnosticSummary{Bytes:len(raw),Truncated:len(raw)>16*1024*1024,ExitFailed:exitErr!=nil,Events:[]semanticClaimDiagnosticEvent{}}
 scan:=raw;if s.Truncated {scan=raw[:16*1024*1024]}
 allowed:=map[string]bool{"TestIntegrationV27IndexedDownloads":true,"TestIntegrationV27IndexedDownloads/semantic-transfer-claim":true}
 for _,n:=range semanticClaimNestedNames {allowed["TestIntegrationV27IndexedDownloads/semantic-transfer-claim/"+n]=true}
 for _,line:=range strings.Split(strings.TrimSpace(string(scan)),"\n") {
  var e struct {Action,Package,Test string};if json.Unmarshal([]byte(line),&e)!=nil{s.Malformed++;continue}
  if e.Package!="photoeditor/backend/internal/gateway" {s.Foreign++;continue}
  if e.Action!="run"&&e.Action!="pass"&&e.Action!="fail"&&e.Action!="skip" {continue}
  if e.Test!=""&&!allowed[e.Test] {s.Foreign++;continue}
  if len(s.Events)<4096 {s.Events=append(s.Events,semanticClaimDiagnosticEvent{e.Action,e.Test})}
 }
 return s
}
func saveSemanticClaimDiagnostic(t *testing.T,root,task string,raw []byte,exitErr error) {
 t.Helper();summary:=semanticClaimDiagnosticSummaryFor(raw,exitErr)
 dir,err:=os.MkdirTemp(filepath.Join(root,"work"),"semantic-claim-nested-private-");if err!=nil {t.Fatal("nested diagnostic directory unavailable")}
 if err=os.Chmod(dir,0700);err!=nil {t.Fatal("nested diagnostic directory permission")}
 saved:=raw;if summary.Truncated {saved=raw[:16*1024*1024]}
 file,err:=os.OpenFile(filepath.Join(dir,"stdout.private.jsonl"),os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if err!=nil {t.Fatal("nested diagnostic file unavailable")}
 _,writeErr:=file.Write(saved);syncErr:=file.Sync();closeErr:=file.Close();if writeErr!=nil||syncErr!=nil||closeErr!=nil {t.Fatal("nested diagnostic persistence failed")}
 safe,err:=json.Marshal(summary);if err!=nil {t.Fatal("nested diagnostic summary failed")}
 if err=os.WriteFile(filepath.Join(dir,"summary.json"),safe,0600);err!=nil {t.Fatal("nested diagnostic summary persistence failed")}
 t.Logf("SEMANTIC_CLAIM_NESTED_DIAGNOSTIC task=%s private_dir=%s summary=%s",task,dir,safe)
}
func TestSemanticClaimDiagnosticSummary(t *testing.T) {
 raw:=[]byte("{\"Action\":\"fail\",\"Package\":\"photoeditor/backend/internal/gateway\",\"Test\":\"TestIntegrationV27IndexedDownloads\",\"Output\":\"SECRET_SENTINEL\"}\n{\"Action\":\"fail\",\"Package\":\"SECRET_SENTINEL\",\"Test\":\"SECRET_SENTINEL\"}\nnot-json\n")
 s:=semanticClaimDiagnosticSummaryFor(raw,context.Canceled);if !s.ExitFailed||s.Malformed!=1||s.Foreign!=1||len(s.Events)!=1||s.Events[0].Action!="fail" {t.Fatal("closed diagnostic summary mismatch")}
 b,_:=json.Marshal(s);if strings.Contains(string(b),"SECRET_SENTINEL") {t.Fatal("private output leaked")}
 large:=[]byte(strings.Repeat("x",16*1024*1024+1));v:=semanticClaimDiagnosticSummaryFor(large,nil);if !v.Truncated||v.Bytes!=len(large)||v.ExitFailed {t.Fatal("diagnostic bound mismatch")}
}

func TestSemanticClaimDiagnosticPersistence(t *testing.T) {
 root:=t.TempDir();if err:=os.Mkdir(filepath.Join(root,"work"),0700);err!=nil {t.Fatal(err)}
 raw:=[]byte("private-sentinel");saveSemanticClaimDiagnostic(t,root,"owned-task",raw,nil)
 dirs,err:=filepath.Glob(filepath.Join(root,"work","semantic-claim-nested-private-*"));if err!=nil||len(dirs)!=1 {t.Fatal("owned diagnostic directory missing")}
 st,err:=os.Stat(dirs[0]);if err!=nil||st.Mode().Perm()!=0700 {t.Fatal("directory not private")}
 p:=filepath.Join(dirs[0],"stdout.private.jsonl");st,err=os.Stat(p);if err!=nil||st.Mode().Perm()!=0600 {t.Fatal("raw not private")}
 got,err:=os.ReadFile(p);if err!=nil||string(got)!=string(raw) {t.Fatal("raw diagnostic not retained")}
 safe,err:=os.ReadFile(filepath.Join(dirs[0],"summary.json"));if err!=nil||strings.Contains(string(safe),"private-sentinel") {t.Fatal("summary leaked private output")}
}
