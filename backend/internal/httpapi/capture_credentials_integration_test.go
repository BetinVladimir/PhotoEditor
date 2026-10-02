package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2capture"
)

func TestIntegrationCaptureCredentialsHTTP(t *testing.T) {
	h, db, ownerToken, tenant := httpFixture(t)
	var project uuid.UUID
	if err := db.QueryRow(context.Background(), `INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Capture credentials') RETURNING id`, tenant).Scan(&project); err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/tenants/" + tenant.String()
	w := request(h, "POST", base+"/capture-devices", `{"project_id":"`+project.String()+`","label":"Camera","public_key":"`+base64.RawURLEncoding.EncodeToString(public)+`"}`, ownerToken, "register-device", "")
	if w.Code != 201 {
		t.Fatal("register", w.Code, w.Body.String())
	}
	var registered struct {
		ID uuid.UUID `json:"id"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &registered); err != nil || registered.ID == uuid.Nil {
		t.Fatal("device response", w.Body.String(), err)
	}
	devicePath := base + "/capture-devices/" + registered.ID.String()
	w = request(h, "POST", devicePath+"/challenges", `{"device_id":"`+registered.ID.String()+`"}`, "", "", "")
	if w.Code != 200 {
		t.Fatal("challenge", w.Code, w.Body.String())
	}
	var challenge v2capture.ChallengeResponse
	if err = json.Unmarshal(w.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	epoch, err := strconv.ParseInt(challenge.CredentialEpoch, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := v2capture.SignedChallenge(registered.ID, challenge.ChallengeID, challenge.Nonce, epoch, "http://localhost:3333")
	if err != nil {
		t.Fatal(err)
	}
	wrongOrigin, err := v2capture.SignedChallenge(registered.ID, challenge.ChallengeID, challenge.Nonce, epoch, "https://other-server.example")
	if err != nil {
		t.Fatal(err)
	}
	wrongBody := `{"challenge_id":"` + challenge.ChallengeID.String() + `","signature":"` + base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, wrongOrigin)) + `"}`
	w = request(h, "POST", devicePath+"/token", wrongBody, "", "", "")
	if w.Code != 401 {
		t.Fatal("signature for another origin accepted", w.Code, w.Body.String())
	}
	signature := base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, signed))
	body := `{"challenge_id":"` + challenge.ChallengeID.String() + `","signature":"` + signature + `"}`
	w = request(h, "POST", devicePath+"/token", body, "", "", "")
	if w.Code != 200 {
		t.Fatal("token", w.Code, w.Body.String())
	}
	var credentials v2capture.TokenResponse
	if err = json.Unmarshal(w.Body.Bytes(), &credentials); err != nil || credentials.AccessToken == "" {
		t.Fatal("token response", err)
	}
	w = request(h, "POST", devicePath+"/token", body, "", "", "")
	if w.Code != 401 {
		t.Fatal("challenge replay accepted", w.Code, w.Body.String())
	}
	w = request(h, "GET", base+"/projects", "", credentials.AccessToken, "", "")
	if w.Code != 401 {
		t.Fatal("capture token entered user API", w.Code, w.Body.String())
	}
	receiptPath := base + "/shoot-sessions/" + uuid.NewString() + "/capture-receipts"
	w = request(h, "POST", receiptPath, `{}`, ownerToken, "receipt", "")
	if w.Code != 401 {
		t.Fatal("user token entered device API", w.Code, w.Body.String())
	}
	w = request(h, "POST", receiptPath, `{}`, credentials.AccessToken, "receipt", "")
	if w.Code != 422 {
		t.Fatal("valid device token did not reach payload validation", w.Code, w.Body.String())
	}
	w = request(h, "POST", devicePath+"/challenges", `{"device_id":"`+registered.ID.String()+`"}`, "", "", "")
	if w.Code != 200 {
		t.Fatal("second challenge", w.Code, w.Body.String())
	}
	var expired v2capture.ChallengeResponse
	if err = json.Unmarshal(w.Body.Bytes(), &expired); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(context.Background(), `UPDATE photo.capture_challenges SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, tenant, expired.ChallengeID); err != nil {
		t.Fatal(err)
	}
	expiredSigned, err := v2capture.SignedChallenge(registered.ID, expired.ChallengeID, expired.Nonce, epoch, "http://localhost:3333")
	if err != nil {
		t.Fatal(err)
	}
	expiredBody := `{"challenge_id":"` + expired.ChallengeID.String() + `","signature":"` + base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, expiredSigned)) + `"}`
	w = request(h, "POST", devicePath+"/token", expiredBody, "", "", "")
	if w.Code != 401 {
		t.Fatal("expired challenge accepted", w.Code, w.Body.String())
	}
	t.Run("python_agent_live_challenge", func(t *testing.T) {
		root, err := filepath.Abs("../../..")
		if err != nil {
			t.Fatal(err)
		}
		python := os.Getenv("PHOTO_TEST_STUDIO_PYTHON")
		if python == "" {
			python = filepath.Join(root, ".runtime", "studio-agent-venv", "bin", "python")
		}
		if _, err := os.Stat(python); err != nil {
			t.Skip("Studio Agent Python runtime unavailable; set PHOTO_TEST_STUDIO_PYTHON")
		}
		session, collection, upload, sha := captureVerifiedReceiptFixture(t, db, tenant, project, registered.ID)
		server := httptest.NewServer(h)
		defer server.Close()
		payload, err := json.Marshal(map[string]string{
			"origin": server.URL, "tenant": tenant.String(), "project": project.String(),
			"session": session.String(), "collection": collection.String(), "device": registered.ID.String(),
			"upload": upload.String(), "sha": sha,
			"seed": base64.StdEncoding.EncodeToString(private.Seed()),
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		script := `import base64,json,sys
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from studio_agent.http import HTTPClient,APIError
from studio_agent.journal import Entry
c=json.load(sys.stdin)
key=Ed25519PrivateKey.from_private_bytes(base64.b64decode(c["seed"]))
client=HTTPClient(c["origin"],c["tenant"],c["project"],c["session"],c["collection"],c["device"],key.sign,[c["origin"]],server_origin="http://localhost:3333",development=True)
client._authenticate()
if not client.token: raise RuntimeError("missing capture token")
entry=Entry(c["device"],1,"frame.jpg",3,1,"receipt","invalid",c["collection"],0,None)
try:
 client.receipt(entry,"python-agent-live-receipt")
 raise RuntimeError("invalid payload accepted")
except APIError as error:
 if error.status!=422 or error.code!="VALIDATION_FAILED": raise
entry=Entry(c["device"],1,"frame.jpg",3,1,"receipt",c["sha"],c["upload"],0,None)
first=client.receipt(entry,"python-agent-positive-receipt")
replay=client.receipt(entry,"python-agent-positive-receipt")
if first!=replay: raise RuntimeError("receipt replay changed")
second_key=client.receipt(entry,"python-agent-same-sequence-second-key")
if second_key!=first: raise RuntimeError("same sequence duplicated receipt")
changed=Entry(c["device"],1,"frame.jpg",3,1,"receipt","d"*64,c["upload"],0,None)
try:
 client.receipt(changed,"python-agent-sequence-conflict")
 raise RuntimeError("changed sequence content accepted")
except APIError as error:
 if error.status!=409: raise
print("agent-challenge-and-positive-receipt-replay-ok")
`
		command := exec.CommandContext(ctx, python, "-c", script)
		command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "apps", "studio-agent"))
		command.Stdin = strings.NewReader(string(payload))
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("live agent interoperability: %v: %s", err, output)
		}
		if strings.TrimSpace(string(output)) != "agent-challenge-and-positive-receipt-replay-ok" {
			t.Fatalf("unexpected agent result: %s", output)
		}
		var count int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM photo.capture_receipts r JOIN photo.capture_receipt_assets a ON a.tenant_id=r.tenant_id AND a.receipt_id=r.id WHERE r.tenant_id=$1 AND r.device_id=$2 AND r.sequence=1 AND r.upload_id=$3`, tenant, registered.ID, upload).Scan(&count); err != nil || count != 1 {
			t.Fatal("agent receipt binding count", count, err)
		}

		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM photo.capture_assessment_jobs j JOIN photo.capture_receipts r ON r.tenant_id=j.tenant_id AND r.id=j.receipt_id WHERE r.tenant_id=$1 AND r.device_id=$2 AND r.sequence=1`, tenant, registered.ID).Scan(&count); err != nil || count != 1 {
			t.Fatal("agent QA queue count", count, err)
		}

	})
	w = request(h, "POST", devicePath+"/revoke", `{"expected_revision":"1"}`, ownerToken, "revoke-device", "")
	if w.Code != 200 {
		t.Fatal("device revoke", w.Code, w.Body.String())
	}
	w = request(h, "POST", receiptPath, `{}`, credentials.AccessToken, "receipt", "")
	if w.Code != 401 {
		t.Fatal("revoked device token accepted", w.Code, w.Body.String())
	}
}
