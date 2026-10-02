package v2region

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type deniedRegionTx struct {
	pgx.Tx
	queries []string
}
type deniedRegionRow struct{}

func (deniedRegionRow) Scan(dest ...any) error { *dest[0].(*bool) = false; return nil }
func (tx *deniedRegionTx) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, q)
	return deniedRegionRow{}
}

func TestRegionSealDeniedAuthorityDoesNotRegisterInputs(t *testing.T) {
	plan, _, params, profile := regionNodeFixture(t)
	var document regionPlanDocument
	_ = json.Unmarshal(plan.JSON(), &document)
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	key := "region-seal-denied"
	run, _ := v2input.RunID(document.TenantID, who.UserID, key)
	workflow, _ := v2input.WorkflowID(run)
	task, _ := v2input.TaskID(workflow, "region.preview", 0)
	raw, _ := json.Marshal(profile)
	node, err := plan.CompileNode(task, params, plan.InputRequirements(), raw)
	if err != nil {
		t.Fatal(err)
	}
	tx := &deniedRegionTx{}
	err = node.SealInTransaction(context.Background(), tx, who, key, uuid.New(), regionSHA, 1024, v2input.Built{})
	if err != ErrRegionInvalid || len(tx.queries) != 1 || !strings.Contains(tx.queries[0], "v26_region_seal_scope") {
		t.Fatal("denied scope reached registration", err, tx.queries)
	}
	tx.queries = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = node.SealInTransaction(ctx, tx, who, key, uuid.New(), regionSHA, 1024, v2input.Built{}); err != context.Canceled || len(tx.queries) != 0 {
		t.Fatal("cancelled seal reached SQL", err)
	}
}
