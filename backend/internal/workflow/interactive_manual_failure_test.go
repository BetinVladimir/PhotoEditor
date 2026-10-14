package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"photoeditor/backend/internal/canonical"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
)

type manualFailureQueries struct{ expansionQueries }

func (*manualFailureQueries) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected row stream")
}

func TestInvalidManualFailureKeepsCauseAndRefusesStaleLease(t *testing.T) {
	cause := errors.New("invalid vector geometry")
	lease := InteractiveLease{TenantID: uuid.New(), RunID: uuid.New(), TaskID: uuid.New(), Token: uuid.New()}
	lease.Params = json.RawMessage(`{"shapes":[]}`)
	lease.ParamsSHA, _ = canonical.Hash(lease.Params)
	db := &manualFailureQueries{}
	c := InteractiveCoordinator{Store: InteractiveStore{DB: db}}
	db.row = expansionRow{values: []any{false}}
	worked, err := c.failInvalidManual(context.Background(), lease, "MANUAL_GEOMETRY_INVALID", cause)
	if !worked || !errors.Is(err, cause) || !errors.Is(err, artifactjson.ErrStale) {
		t.Fatal("stale failure lost diagnostic or fence", err)
	}
	if len(db.args) != 7 || db.args[4] != lease.ParamsSHA || db.args[5] != string(lease.Params) {
		t.Fatal("terminal failure omitted claimed Params fence")
	}
	db.row = expansionRow{values: []any{true}}
	worked, err = c.failInvalidManual(context.Background(), lease, "MANUAL_GEOMETRY_INVALID", cause)
	if !worked || !errors.Is(err, cause) || errors.Is(err, artifactjson.ErrStale) {
		t.Fatal("recorded failure lost diagnostic", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db.sql = ""
	worked, err = c.failInvalidManual(ctx, lease, "MANUAL_GEOMETRY_INVALID", cause)
	if !worked || !errors.Is(err, context.Canceled) || db.sql != "" {
		t.Fatal("cancellation wrote terminal state", err)
	}
}
