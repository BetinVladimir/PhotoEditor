package workflow

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/review"
)

// Opt-in disposable-clone test. The fixture owner creates relational rows;
// every store call below uses the actual restricted production LOGIN/options.
// It never creates a role, runs workers, or claims artifact-byte qualification.
func TestApplyLocatorRestrictedNonemptyIntegration(t *testing.T) {
	dsn, path := os.Getenv("APPLY_LOCATOR_RESTRICTED_DSN"), os.Getenv("APPLY_LOCATOR_FIXTURE")
	if dsn == "" || path == "" {
		t.Skip("owned clone scheduler LOGIN fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("fixture connection configuration invalid")
	}
	if !strings.HasPrefix(cfg.Database, "photoeditor_apply_locator_") || cfg.User != "rtx_test_scheduler" {
		t.Fatal("owned clone LOGIN required")
	}
	cfg.RuntimeParams["role"] = "photo_scheduler"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal("fixture connection failed")
	}
	defer conn.Close(ctx)
	if err = database.RequireApplicationRole(ctx, conn, "photo_scheduler"); err != nil {
		t.Fatal("production application role guard", err)
	}
	var current, session string
	if err = conn.QueryRow(ctx, "SELECT current_user,session_user").Scan(&current, &session); err != nil || current != "photo_scheduler" || session != "rtx_test_scheduler" {
		t.Fatal("restricted LOGIN role binding")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 16384 {
		t.Fatal("fixture binding unreadable")
	}
	var f struct {
		QueuedTenant    uuid.UUID `json:"queued_tenant"`
		QueuedRun       uuid.UUID `json:"queued_run"`
		QueuedToken     uuid.UUID `json:"queued_token"`
		QueuedMaster    uuid.UUID `json:"queued_master"`
		QueuedMasterSHA string    `json:"queued_master_sha"`
		QueuedStyle     uuid.UUID `json:"queued_style"`
		QueuedStyleSHA  string    `json:"queued_style_sha"`

		Tenant      uuid.UUID `json:"tenant"`
		Run         uuid.UUID `json:"run"`
		Token       uuid.UUID `json:"token"`
		Master      uuid.UUID `json:"master"`
		MasterSHA   string    `json:"master_sha"`
		Style       uuid.UUID `json:"style"`
		StyleSHA    string    `json:"style_sha"`
		Task        uuid.UUID `json:"task"`
		Proposal    uuid.UUID `json:"proposal"`
		ProposalSHA string    `json:"proposal_sha"`
		Report      uuid.UUID `json:"report"`
	}
	if json.Unmarshal(raw, &f) != nil {
		t.Fatal("fixture binding malformed")
	}
	l := ApplyLease{TenantID: f.Tenant, RunID: f.Run, Token: f.Token, GraphVersion: 4, ItemCount: 1, Phase: "defects_auto_repair"}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	store := ApplyStore{DB: tx}
	queuedLease := ApplyLease{TenantID: f.QueuedTenant, RunID: f.QueuedRun, Token: f.QueuedToken, GraphVersion: 4, ItemCount: 1, Phase: "resolve"}
	inputs, err := store.Inputs(ctx, queuedLease)
	if err != nil || len(inputs) != 1 || inputs[0].Style == nil || inputs[0].Style.ArtifactID != f.QueuedStyle || inputs[0].Style.SHA256 != f.QueuedStyleSHA {
		t.Fatal("real nonempty Inputs locator path", err)
	}
	source, err := store.Artifact(ctx, queuedLease, review.ArtifactRef{ID: f.QueuedMaster, SHA256: f.QueuedMasterSHA})
	if err != nil || source.ArtifactID != f.QueuedMaster || source.VersionID == "" {
		t.Fatal("real source locator path", err)
	}
	proposal := applyOutput{TaskID: f.Task, Artifact: styleArtifact{ArtifactID: f.Proposal, SHA256: f.ProposalSHA}}
	report, err := store.conservativeProposalReport(ctx, l, proposal)
	if err != nil || report.ArtifactID != f.Report {
		t.Fatal("real conservative sibling locator path", err)
	}
	var table bool
	if err = tx.QueryRow(ctx, "SELECT has_table_privilege(current_user,'photo.artifacts','SELECT')").Scan(&table); err != nil || table {
		t.Fatal("no artifact table grant")
	}
	bad := l
	bad.Token = uuid.New()
	if _, err = store.Artifact(ctx, bad, review.ArtifactRef{ID: f.Master, SHA256: f.MasterSHA}); err == nil {
		t.Fatal("foreign lease was admitted")
	}
}
