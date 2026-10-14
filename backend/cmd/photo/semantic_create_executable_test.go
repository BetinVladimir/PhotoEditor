package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticproposal"
	"photoeditor/backend/internal/v2input"
)

// Invoked only after the owned fixture has committed the original parent.
// This is the production executable (main/run), not an alternate HTTP server.
func probeSemanticCreateExecutable(t *testing.T, ctx context.Context, root, directory, deployment, dsn, schedulerDSN string, objects objectstore.Config, tenant, actor, session uuid.UUID, intent v2input.SemanticEditIntent) {
	t.Helper()
	binary := filepath.Join(directory, "photo")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatal("create executable build failed", err, string(output))
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("create executable signing key unavailable")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal("create executable auth secret unavailable")
	}
	encode := base64.StdEncoding.EncodeToString
	keys := auth.FileKeys{ActiveJWT: "fixture", JWTSeed: encode(private.Seed()), JWTPublic: map[string]string{"fixture": encode(public)}, OTPPepper: encode(secret), ActiveMail: "fixture", Mail: map[string]string{"fixture": encode(secret)}}
	keyBody, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(directory, "auth.json")
	if os.WriteFile(keyFile, keyBody, 0600) != nil {
		t.Fatal("create executable auth file unavailable")
	}
	owner, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("create executable fixture database unavailable")
	}
	defer owner.Close()
	var epoch int64
	if owner.QueryRow(ctx, "SELECT auth_epoch FROM photo.users WHERE id=$1", actor).Scan(&epoch) != nil {
		t.Fatal("create executable actor unavailable")
	}
	signer := auth.TokenSigner{Active: "fixture", Private: private, Public: map[string]ed25519.PublicKey{"fixture": public}, Issuer: "fixture", Audience: "fixture"}
	token, err := signer.Issue(actor, session, epoch)
	if err != nil {
		t.Fatal("create executable access unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("create executable loopback address unavailable")
	}
	address := listener.Addr().String()
	listener.Close()
	command := exec.CommandContext(ctx, binary)
	// Explicit isolated environment: no operator's unrelated optional service
	// configurations or production endpoint can leak into this owned fixture.
	command.Env = []string{
		"PHOTO_ROLE=api", "APP_ENV=development", "HTTP_LISTEN=" + address,
		"DATABASE_URL=" + dsn, "AUTH_DATABASE_URL=" + dsn, "AUTH_KEYS_FILE=" + keyFile,
		"JWT_ISSUER=fixture", "JWT_AUDIENCE=fixture",
		"PHOTO_ICC_ROOT=" + filepath.Join(root, "resources", "icc"),
		"PHOTO_V27_CREATE_CONFIG=" + deployment, "V27_CREATE_SCHEDULER_DATABASE_URL=" + schedulerDSN,
		"S3_ENDPOINT=" + objects.Endpoint, "S3_PUBLIC_ENDPOINT=" + objects.PublicEndpoint,
		"S3_BUCKET=" + objects.Bucket, "S3_ACCESS_KEY=" + objects.AccessKey,
		"S3_SECRET_KEY=" + objects.SecretKey, "S3_REGION=" + objects.Region,
	}
	if err := command.Start(); err != nil {
		t.Fatal("create executable failed to start")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	finished := false
	defer func() {
		if !finished {
			command.Process.Kill()
			<-done
		}
	}()
	client := &http.Client{Timeout: 10 * time.Second}
	base := "http://" + address
	ready := false
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	startup, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	for !ready {
		select {
		case err := <-done:
			finished = true
			t.Fatal("create executable exited during startup", err)
		case <-startup.Done():
			t.Fatal("create executable readiness timed out")
		case <-ticker.C:
			r, err := http.NewRequestWithContext(startup, http.MethodGet, base+"/health/ready", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(r)
			if err == nil {
				ready = response.StatusCode == 200
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
		}
	}
	body, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := semanticproposal.CompileRequest(tenant, actor, "proposal-publication", body)
	if err != nil {
		t.Fatal("create executable original request invalid")
	}
	var first []byte
	for retry := 0; retry < 2; retry++ {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/tenants/"+tenant.String()+"/semantic-intents", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Photo-Client", "native")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", "proposal-publication")
		response, err := client.Do(r)
		if err != nil {
			t.Fatal("create executable replay request failed")
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 65537))
		response.Body.Close()
		var ticket v2input.JobTicket
		if readErr != nil || len(raw) > 65536 || response.StatusCode != 202 || json.Unmarshal(raw, &ticket) != nil || ticket.RunID != compiled.RunID || ticket.WorkflowID != compiled.WorkflowID || ticket.State != "queued" {
			t.Fatal("create executable replay receipt invalid", response.StatusCode)
		}
		if retry == 0 {
			first = raw
		} else if !bytes.Equal(first, raw) {
			t.Fatal("create executable replay receipt changed")
		}
	}
	resolve, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/tenants/"+tenant.String()+"/runs/"+compiled.RunID.String()+"/semantic-intent", nil)
	if err != nil {
		t.Fatal(err)
	}
	resolve.Header.Set("Authorization", "Bearer "+token)
	resolved, err := client.Do(resolve)
	if err != nil {
		t.Fatal("semantic parent resolution failed")
	}
	resolvedBody, readErr := io.ReadAll(io.LimitReader(resolved.Body, 65537))
	resolved.Body.Close()
	var identity struct {
		RunID      uuid.UUID  `json:"run_id"`
		WorkflowID uuid.UUID  `json:"workflow_id"`
		IntentID   *uuid.UUID `json:"intent_id"`
		Revision   string     `json:"revision"`
		State      string     `json:"state"`
	}
	// Draft revision 1 advances exactly once on sealed admission.
	if readErr != nil || resolved.StatusCode != 200 || json.Unmarshal(resolvedBody, &identity) != nil || identity.RunID != compiled.RunID || identity.WorkflowID != compiled.WorkflowID || identity.IntentID != nil || identity.Revision != "2" || identity.State != "queued" {
		t.Fatal("pending semantic parent resolution invalid", resolved.StatusCode)
	}
	for _, negative := range []struct {
		run           uuid.UUID
		authenticated bool
		status        int
	}{
		{compiled.RunID, false, 401}, {uuid.New(), true, 404},
	} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/tenants/"+tenant.String()+"/runs/"+negative.run.String()+"/semantic-intent", nil)
		if err != nil {
			t.Fatal(err)
		}
		if negative.authenticated {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("semantic parent negative request failed")
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != negative.status {
			t.Fatal("semantic parent negative guard", response.StatusCode)
		}
	}
	if command.Process.Signal(syscall.SIGTERM) != nil {
		t.Fatal("create executable shutdown signal failed")
	}
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal("create executable graceful shutdown failed", err)
		}
	case <-ctx.Done():
		t.Fatal("create executable shutdown timed out")
	}
	t.Log("V27_CREATE_EXECUTABLE_REPLAY_SHUTDOWN_OK")
}
