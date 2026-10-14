package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"testing"
)

// Controlled terminal metadata only: this does not claim worker execution,
// output finalization or pixel adoption. All changes roll back in this probe.
func assertExposureRegisteredInputEvidence(t *testing.T, ctx context.Context, owner *pgxpool.Pool, tenant, child, task uuid.UUID) {
	t.Helper()
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var params uuid.UUID
	var digest string
	if err = tx.QueryRow(ctx, "SELECT a.id,a.sha256 FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id WHERE t.tenant_id=$1 AND t.id=$2", tenant, task).Scan(&params, &digest); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE photo.runs SET status='running' WHERE tenant_id=$1 AND id=$2", tenant, child); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE photo.tasks SET attempt=1,fencing_token=1 WHERE tenant_id=$1 AND id=$2", tenant, task); err != nil {
		t.Fatal(err)
	}
	outputs, _ := canonical.Marshal([]struct {
		ArtifactID uuid.UUID `json:"artifact_id"`
		SHA        string    `json:"sha256"`
	}{{params, digest}})
	var result uuid.UUID
	if err = tx.QueryRow(ctx, "INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,$3::jsonb,'{}',$4) RETURNING id", tenant, task, json.RawMessage(outputs), digest).Scan(&result); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE photo.tasks SET state='succeeded',canonical_result_id=$3 WHERE tenant_id=$1 AND id=$2", tenant, task, result); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, "SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2 ORDER BY artifact_id", tenant, task)
	if err != nil {
		t.Fatal(err)
	}
	type input struct {
		id  uuid.UUID
		sha string
	}
	var inputs []input
	for rows.Next() {
		var entry input
		if err = rows.Scan(&entry.id, &entry.sha); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		inputs = append(inputs, entry)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(inputs) < 8 {
		t.Fatal("incomplete registered input fixture")
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	for _, entry := range inputs {
		var actualSHA, source, version string
		if err = tx.QueryRow(ctx, "SELECT sha256,source,object_version FROM photo.v27_exposure_adoption_artifact($1,$2,1,1,$3)", tenant, task, entry.id).Scan(&actualSHA, &source, &version); err != nil || actualSHA != entry.sha || source != "input" || version == "" || version == "null" {
			t.Fatal("registered exposure input evidence", err)
		}
	}
	var count int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM photo.v27_exposure_adoption_artifact($1,$2,1,2,$3)", tenant, task, inputs[0].id).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale input fence disclosed", err)
	}
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM photo.v27_exposure_adoption_artifact($1,$2,1,1,$3)", uuid.New(), task, inputs[0].id).Scan(&count); err != nil || count != 0 {
		t.Fatal("foreign input tenant disclosed", err)
	}
}
