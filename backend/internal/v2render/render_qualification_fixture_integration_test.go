package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
	"time"
)

// Controlled relational qualification evidence only: no GPU measurements and
// no report object upload. This helper runs only in the isolated test database.
func seedRenderQualificationMetadata(t *testing.T, ctx context.Context, db *pgxpool.Pool, evidence renderBatchRestartEvidence, model string, choice RenderExecutionChoice) (func(), uuid.UUID) {
	t.Helper()
	var enabled bool
	if err := db.QueryRow(ctx, "SELECT v2_enabled FROM photo.operation_registry WHERE operation='render.compose' AND version='1'").Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO photo.v2_qualification_operators(operator_id,enabled) VALUES($1,true) ON CONFLICT(operator_id) DO NOTHING", evidence.Actor.UserID); err != nil {
		t.Fatal(err)
	}
	var policy uuid.UUID
	device, dataset, fixture, proof := strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("f", 64)
	if err := db.QueryRow(ctx, `INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,evidence_shas,license_cleared,enabled,admission_enabled)
 VALUES('render.compose','1',$1,$2,$3,$4,$5,$6,ARRAY['quality'],ARRAY[$7],ARRAY[$8],true,true,true) ON CONFLICT(operation,operation_version,model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha,issuer_id) DO UPDATE SET admission_enabled=true,enabled=true,license_cleared=true RETURNING id`, model, choice.RuntimeSHA, choice.Backend, device, dataset, evidence.Actor.UserID, fixture, proof).Scan(&policy); err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := db.Exec(stopCtx, "UPDATE photo.operation_registry SET v2_enabled=$1 WHERE operation='render.compose' AND version='1'", enabled); err != nil {
			t.Error("restore fixture operation", err)
		}
		if _, err := db.Exec(stopCtx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", policy); err != nil {
			t.Error("disable fixture policy", err)
		}
	}
	t.Cleanup(cleanup)
	body, err := canonical.Marshal(map[string]any{"schema_version": 1, "operation": "render.compose", "version": "1", "model_lock_sha": model, "runtime_lock_sha": choice.RuntimeSHA, "device_profile_sha": device, "dataset_sha": dataset, "input_fixture_shas": []string{fixture}, "checks": []any{map[string]any{"id": "quality", "passed": true, "evidence_sha": proof}}, "metrics": map[string]any{"p50_ms": 10, "p95_ms": 20, "peak_vram_bytes": 0, "peak_rss_bytes": 0, "scratch_bytes": 0}, "status": "passed", "issuer_id": evidence.Actor.UserID, "created_at": "2026-09-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(json.RawMessage(body))
	if err != nil {
		t.Fatal(err)
	}
	var artifact uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES($1,'qualification-metadata-fixture',$2,'fixture-v1',$3,$4,'application/json','ready',clock_timestamp()) RETURNING id`, evidence.Tenant, uuid.NewString(), sha, len(body)).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_maintenance"); err != nil {
		t.Fatal(err)
	}
	var qualification uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT photo.import_v2_qualification($1,$2,$3,$4,$5,$6,$7)", evidence.Tenant, evidence.Project, artifact, sha, evidence.Actor.UserID, choice.Backend, string(body)).Scan(&qualification); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='render.compose' AND version='1'"); err != nil {
		t.Fatal(err)
	}
	return cleanup, policy
}
