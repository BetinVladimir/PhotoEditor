package semanticreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type exposureInputTx struct {
	pgx.Tx
	calls        []string
	args         [][]any
	rows, sealed int
	failure      error
	batch        uuid.UUID
	failAt       int
}
type exposureInputRow struct {
	value any
	err   error
}

func (r exposureInputRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch target := dest[0].(type) {
	case *int:
		*target = r.value.(int)
	case *uuid.UUID:
		*target = r.value.(uuid.UUID)
	default:
		return ErrInvalid
	}
	return nil
}
func (tx *exposureInputTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.calls = append(tx.calls, query)
	tx.args = append(tx.args, args)
	var value any = tx.rows
	if strings.Contains(query, "seal_semantic_apply_inputs") {
		value = tx.sealed
	}
	if strings.Contains(query, "create_semantic_apply_draft_shell") {
		value = tx.batch
	}
	failure := tx.failure
	if tx.failAt > 0 && len(tx.calls) != tx.failAt {
		failure = nil
	}
	return exposureInputRow{value: value, err: failure}
}

func checkExposureInputAdmission(t *testing.T, tenant, actor, child uuid.UUID, prepared PreparedReview,
	targets []LockedTarget, request ApplyDecision, decision PublishedDecision, input ExposureTaskInputs,
	published PreparedExposureTask, expected ExposureCommitInputs) {
	t.Helper()
	project := uuid.New()
	rows := published.Graph.Built.Count + len(published.Graph.Built.Chunks) + 1
	invoke := func(ctx context.Context, tx pgx.Tx, p PreparedExposureTask) (ExposureCommitInputs, error) {
		return RegisterApprovedExposureInputsInTransaction(ctx, tx, tenant, project, actor, child, prepared, targets, request, decision, []ExposureTaskInputs{input}, []PreparedExposureTask{p})
	}
	tx := &exposureInputTx{rows: rows, sealed: 1}
	actual, err := invoke(context.Background(), tx, published)
	if err != nil || !bytes.Equal(actual.Inventory, expected.Inventory) || len(tx.calls) != 2 {
		t.Fatal("input admission", err)
	}
	if tx.args[0][0] != tenant || tx.args[0][1] != project || tx.args[0][2] != prepared.Sources.RunID || tx.args[0][3] != child {
		t.Fatal("scope substitution")
	}
	if !bytes.Equal(tx.args[1][4].(json.RawMessage), expected.Review.Params) {
		t.Fatal("Params substitution")
	}
	for _, failed := range []*exposureInputTx{{rows: rows - 1, sealed: 1}, {rows: rows, sealed: 0}, {failure: errors.New("database unavailable")}} {
		result, err := invoke(context.Background(), failed, published)
		if err == nil || result.Nodes != nil {
			t.Fatal("partial admission accepted")
		}
		if failed.rows != rows && len(failed.calls) != 1 {
			t.Fatal("sealed after failed registration")
		}
	}
	changed := published
	changed.Graph.Built.SHA256 = "invalid"
	denied := &exposureInputTx{rows: rows, sealed: 1}
	if _, err := invoke(context.Background(), denied, changed); err == nil || len(denied.calls) != 0 {
		t.Fatal("changed publication reached SQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := invoke(ctx, denied, published); !errors.Is(err, context.Canceled) || len(denied.calls) != 0 {
		t.Fatal("cancel reached SQL")
	}
	intent, session := uuid.New(), uuid.New()
	draftInvoke := func(ctx context.Context, tx pgx.Tx, p PreparedExposureTask) (ExposureCommitInputs, error) {
		return createApprovedExposureDraftInTransaction(ctx, tx, tenant, project, intent, actor, session, child, prepared, targets, request, decision, []ExposureTaskInputs{input}, []PreparedExposureTask{p})
	}
	draftTX := &exposureInputTx{rows: rows, sealed: 1, batch: decision.BatchID}
	draftResult, draftErr := draftInvoke(context.Background(), draftTX, published)
	if draftErr != nil || len(draftTX.calls) != 3 || !bytes.Equal(draftResult.Inventory, expected.Inventory) {
		t.Fatal("draft composition", draftErr)
	}
	if draftTX.args[0][0] != tenant || draftTX.args[0][1] != intent || draftTX.args[0][2] != actor || draftTX.args[0][3] != session || draftTX.args[0][9] != child {
		t.Fatal("draft scope changed")
	}
	if !bytes.Equal(draftTX.args[0][10].(json.RawMessage), expected.Review.Params) || !bytes.Equal(draftTX.args[0][11].(json.RawMessage), expected.Nodes) {
		t.Fatal("draft projections changed")
	}
	for step := 1; step <= 3; step++ {
		denied := &exposureInputTx{rows: rows, sealed: 1, batch: decision.BatchID, failure: errors.New("SQL failure"), failAt: step}
		result, err := draftInvoke(context.Background(), denied, published)
		if err == nil || result.Nodes != nil || len(denied.calls) != step {
			t.Fatal("draft continued after failure", step)
		}
	}
	wrongBatch := &exposureInputTx{rows: rows, sealed: 1, batch: uuid.New()}
	if _, err := draftInvoke(context.Background(), wrongBatch, published); err == nil || len(wrongBatch.calls) != 1 {
		t.Fatal("wrong batch registered inputs")
	}
	noSQL := &exposureInputTx{rows: rows, sealed: 1, batch: decision.BatchID}
	if _, err := draftInvoke(context.Background(), noSQL, changed); err == nil || len(noSQL.calls) != 0 {
		t.Fatal("changed publication created draft")
	}
	if _, err := draftInvoke(ctx, noSQL, published); !errors.Is(err, context.Canceled) || len(noSQL.calls) != 0 {
		t.Fatal("cancel created draft")
	}

}
