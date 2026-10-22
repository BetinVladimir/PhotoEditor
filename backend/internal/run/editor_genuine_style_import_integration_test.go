package run

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/style"
	"strings"
	"testing"
	"time"
)

type genuineStyleImport struct {
	Tenant, Project, Actor, Session, PackageArtifact     uuid.UUID
	PackageSHA, ImportKey, AcceptKey, ReviewCandidateSHA string
	PackageBucket                                        string
	CandidateRun, StyleID                                uuid.UUID
	ExpectedRevision                                     int64
	AcknowledgeLowConfidence                             bool
}

func genuineStyleInput(t *testing.T) (genuineStyleImport, string) {
	t.Helper()
	var f genuineStyleImport
	raw := os.Getenv("PHOTO_EDITOR_STYLE_IMPORT")
	output := os.Getenv("PHOTO_EDITOR_STYLE_OUTPUT")
	if raw == "" && output == "" {
		t.Skip("genuine Style import fixture not configured")
	}
	if raw == "" || output == "" {
		t.Fatal("partial genuine Style import fixture")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || d.Decode(new(any)) != io.EOF || f.Tenant == uuid.Nil || f.Project == uuid.Nil || f.Actor == uuid.Nil || f.Session == uuid.Nil || output == "" {
		t.Fatal("closed actual style import scope required")
	}
	return f, output
}
func genuineStyleAudit(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()
	cfg, e := pgx.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal("private audit DSN")
	}
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	c, e := pgx.ConnectConfig(ctx, cfg)
	if e != nil {
		t.Fatal("private read-only style audit")
	}
	t.Cleanup(func() { c.Close(context.WithoutCancel(ctx)) })
	return c
}
func genuineStyleWrite(t *testing.T, path string, value any) {
	t.Helper()
	raw, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		t.Fatal("exclusive private style receipt")
	}
	defer f.Close()
	if _, e = f.Write(raw); e != nil {
		t.Fatal(e)
	}
	if e = f.Sync(); e != nil {
		t.Fatal(e)
	}
}

// Requires a genuine canonical style.package output or the actual scoped Style
// package publication API receipt. Neither path fabricates an artifacts-ready row.
func TestIntegrationImportGenuineEditorStyle(t *testing.T) {
	f, output := genuineStyleInput(t)
	if f.PackageArtifact == uuid.Nil || f.ImportKey == "" {
		t.Fatal("actual package producer receipt required")
	}
	if _, e := domain.ParseDigest(f.PackageSHA); e != nil {
		t.Fatal("independent package SHA required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()
	audit := genuineStyleAudit(t, ctx)
	var genuine bool
	e := audit.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE a.tenant_id=$1 AND a.id=$2 AND a.sha256=$3 AND a.output_role='style_package' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.bytes BETWEEN 2 AND 104857600 AND t.operation='style.package' AND t.operation_version='1' AND t.state='succeeded' AND EXISTS(SELECT FROM photo.styles original_style WHERE original_style.tenant_id=r.tenant_id AND original_style.id=(r.config_snapshot->>'style_id')::uuid AND original_style.project_id=$4 AND original_style.archived_at IS NULL) AND NOT r.cancel_requested AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256)))`, f.Tenant, f.PackageArtifact, f.PackageSHA, f.Project).Scan(&genuine)
	if e != nil {
		t.Fatal("current package authority read failed", e)
	}
	if !genuine {
		if f.PackageBucket == "" {
			t.Fatal("configured package bucket required for scoped upload")
		}
		// Fresh tenant upload has a distinct actual publication receipt; it is NOT
		// relabelled as a style.package worker result or an accepted Style.
		e = audit.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.style_package_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=$1 AND u.project_id=$2 AND u.actor_id=$3 AND u.artifact_id=$4 AND u.sha256=$5 AND u.state='ready' AND a.state='ready' AND a.object_version=u.object_version AND a.object_key=u.object_key AND a.sha256=u.sha256 AND a.bytes=u.bytes AND a.media_type=u.media_type AND a.bucket=$6 AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))`, f.Tenant, f.Project, f.Actor, f.PackageArtifact, f.PackageSHA, f.PackageBucket).Scan(&genuine)
	}
	if e != nil || !genuine {
		t.Fatal("real canonical package or verified scoped upload not proven", e)
	}
	api := genuineDedicatedPool(t, ctx, "api")
	svc := style.Service{DB: api, Bucket: f.PackageBucket}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	accepted, e := svc.Import(ctx, who, f.Tenant, f.ImportKey, style.ImportRequest{ProjectID: f.Project, Name: "Genuine imported Editor source", UploadArtifactID: f.PackageArtifact})
	if e != nil || accepted.ResourceID == nil || accepted.Status != 202 {
		t.Fatal("actual Style.Import refused", e)
	}
	runID := *accepted.ResourceID
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		var styleID uuid.UUID
		var sha string
		var config, metrics json.RawMessage
		var revision int64
		var low bool
		e = audit.QueryRow(ctx, `SELECT c.style_id,c.config_hash,c.config_json,c.metrics,s.revision,c.low_confidence FROM photo.style_candidates c JOIN photo.styles s ON s.tenant_id=c.tenant_id AND s.id=c.style_id JOIN photo.artifacts a ON a.tenant_id=c.tenant_id AND a.id=c.config_artifact_id AND a.sha256=c.config_hash AND a.state='ready' AND a.object_version IS NOT NULL JOIN photo.tasks t ON t.tenant_id=c.tenant_id AND t.run_id=c.training_run_id AND t.operation='style.import' AND t.state='succeeded' JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE c.tenant_id=$1 AND c.training_run_id=$2 AND c.state='ready' AND NOT r.cancel_requested AND z.review_required AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))`, f.Tenant, runID).Scan(&styleID, &sha, &config, &metrics, &revision, &low)
		if e == nil {
			actual, err := canonical.Hash(config)
			if err != nil || actual != sha {
				t.Fatal("actual candidate hash mismatch")
			}
			genuineStyleWrite(t, output, struct {
				Tenant, Project, StyleID, CandidateRun uuid.UUID
				CandidateSHA                           string
				ExpectedRevision                       int64
				LowConfidence                          bool
				Config, Metrics                        json.RawMessage
				RequiresExplicitReview                 bool
			}{f.Tenant, f.Project, styleID, runID, sha, revision, low, config, metrics, true})
			return
		}
		if e != pgx.ErrNoRows {
			t.Fatal("actual style candidate read", e)
		}
		var status string
		if e = audit.QueryRow(ctx, `SELECT status FROM photo.runs WHERE tenant_id=$1 AND id=$2`, f.Tenant, runID).Scan(&status); e != nil {
			t.Fatal(e)
		}
		if status == "failed" || status == "cancelled" {
			t.Fatal("actual Style import failed or cancelled")
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual style candidate deadline", ctx.Err())
		case <-tick.C:
		}
	}
}

// A separate invocation after actual photographer/operator review. Never
// derives an acceptance decision from successful import or six unpaired photos.
func TestIntegrationAcceptGenuineEditorStyle(t *testing.T) {
	f, output := genuineStyleInput(t)
	if f.StyleID == uuid.Nil || f.CandidateRun == uuid.Nil || f.ExpectedRevision < 1 || f.AcceptKey == "" {
		t.Fatal("actual reviewed candidate scope required")
	}
	if _, e := domain.ParseDigest(f.ReviewCandidateSHA); e != nil {
		t.Fatal("independently reviewed exact candidate SHA required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	audit := genuineStyleAudit(t, ctx)
	var actualSHA string
	if e := audit.QueryRow(ctx, `SELECT c.config_hash FROM photo.style_candidates c JOIN photo.styles s ON s.tenant_id=c.tenant_id AND s.id=c.style_id WHERE c.tenant_id=$1 AND c.style_id=$2 AND c.training_run_id=$3 AND c.state='ready' AND s.project_id=$4 AND s.revision=$5`, f.Tenant, f.StyleID, f.CandidateRun, f.Project, f.ExpectedRevision).Scan(&actualSHA); e != nil || actualSHA != f.ReviewCandidateSHA {
		t.Fatal("reviewed candidate scope changed")
	}
	svc := style.Service{DB: genuineDedicatedPool(t, ctx, "api")}
	response, e := svc.AcceptCandidate(ctx, auth.Identity{UserID: f.Actor, SessionID: f.Session}, f.Tenant, f.StyleID, f.AcceptKey, style.AcceptStyleRequest{CandidateRunID: f.CandidateRun, ExpectedStyleRevision: f.ExpectedRevision, Decision: "accept", AcknowledgeLowConfidence: f.AcknowledgeLowConfidence})
	if e != nil || response.ResourceID == nil || response.Status != 201 {
		t.Fatal("actual explicit candidate acceptance refused", e)
	}
	genuineStyleWrite(t, output, struct {
		Version uuid.UUID
		Body    json.RawMessage
	}{*response.ResourceID, response.Body})
}
