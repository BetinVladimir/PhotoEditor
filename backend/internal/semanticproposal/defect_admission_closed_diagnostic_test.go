package semanticproposal

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
)

type closedDiagnosticRow struct{ fail bool }

func (r closedDiagnosticRow) Scan(dest ...any) error {
	if r.fail {
		return errors.New("PRIVATE QUERY BODY MUST NOT ESCAPE")
	}
	for i, d := range dest {
		*d.(*bool) = i != 2
	}
	return nil
}

type closedDiagnosticTx struct {
	pgx.Tx
	beginErr, rollbackErr  error
	fail                   bool
	failAt                 int
	began, queried, rolled int
	child                  *closedDiagnosticTx
}

func (t *closedDiagnosticTx) Begin(context.Context) (pgx.Tx, error) {
	t.began++
	if t.beginErr != nil {
		return nil, t.beginErr
	}
	return t.child, nil
}
func (t *closedDiagnosticTx) QueryRow(context.Context, string, ...any) pgx.Row {
	t.queried++
	return closedDiagnosticRow{t.fail || t.failAt == t.queried}
}
func (t *closedDiagnosticTx) Rollback(ctx context.Context) error {
	t.rolled++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return t.rollbackErr
}
func TestDefectAdmissionClosedDiagnosticPreservesRefusal(t *testing.T) {
	for _, scenario := range []string{"facts", "begin", "query", "rollback", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			original := errors.New("original refusal")
			child := &closedDiagnosticTx{}
			tx := &closedDiagnosticTx{child: child}
			ctx := context.Background()
			switch scenario {
			case "begin":
				tx.beginErr = errors.New("PRIVATE begin")
			case "query":
				child.fail = true
			case "rollback":
				child.rollbackErr = errors.New("PRIVATE rollback")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			got := defectAdmissionClosedDiagnostic(ctx, tx, uuid.New(), uuid.New(), "PRIVATE evidence", original)
			if !errors.Is(got, original) {
				t.Fatal("original identity lost")
			}
			if strings.Contains(got.Error(), "PRIVATE") {
				t.Fatal("private diagnostic escaped")
			}
			want := 2
			if scenario == "begin" {
				want = 0
			}
			if scenario == "rollback" || scenario == "cancelled" {
				want = 1
			}
			if child.rolled != want {
				t.Fatal("savepoint restoration count", child.rolled, want)
			}
			if scenario == "facts" && !strings.Contains(got.Error(), "roots=false") {
				t.Fatal("closed false predicate absent")
			}
		})
	}
}
func TestDefectAdmissionClosedDiagnosticNoSuccessQueries(t *testing.T) {
	tx := &closedDiagnosticTx{}
	if defectAdmissionClosedDiagnostic(context.Background(), tx, uuid.Nil, uuid.Nil, "", nil) != nil || tx.began != 0 {
		t.Fatal("success path changed")
	}
}

func TestDefectAdmissionClosedDiagnosticCatalogDeniedRetainsPureFacts(t *testing.T) {
	original := errors.New("refusal")
	child := &closedDiagnosticTx{failAt: 2}
	tx := &closedDiagnosticTx{child: child}
	got := defectAdmissionClosedDiagnostic(context.Background(), tx, uuid.New(), uuid.New(), "PRIVATE evidence", original)
	for _, part := range []string{"pure_status=observed", "closure=true", "roots=false", "catalog_status=query_unavailable"} {
		if !strings.Contains(got.Error(), part) {
			t.Fatal("missing closed observation", part)
		}
	}
	if child.rolled != 2 || !errors.Is(got, original) {
		t.Fatal("restoration/original lost")
	}
}

type diagnosticStateError struct{ code string }

func (e diagnosticStateError) Error() string    { return "PRIVATE SQL body" }
func (e diagnosticStateError) SQLState() string { return e.code }
func TestDefectAdmissionDiagnosticUnknownIsNotFalse(t *testing.T) {
	for _, status := range []string{"query_unavailable", "query_permission_denied", "begin_unavailable", "not_attempted", "rollback_unavailable"} {
		for _, value := range []bool{false, true} {
			if defectDiagnosticFact(status, value) != "unknown" {
				t.Fatal("invented observation", status)
			}
		}
	}
	if defectDiagnosticFact("observed", true) != "true" || defectDiagnosticFact("observed", false) != "false" {
		t.Fatal("observed fact changed")
	}
}
func TestDefectAdmissionDiagnosticClosedErrorCategories(t *testing.T) {
	for _, row := range []struct{ code, want string }{{"42501", "permission_denied"}, {"57014", "cancelled"}, {"25P02", "transaction_aborted"}, {"42601", "invalid_query"}, {"PRIVATE", "unavailable"}} {
		for _, stage := range []string{"begin", "query", "rollback"} {
			got := defectDiagnosticErrorStatus(stage, fmt.Errorf("PRIVATE outer: %w", diagnosticStateError{row.code}))
			if got != stage+"_"+row.want || strings.Contains(got, "PRIVATE") {
				t.Fatal("unsafe category", got)
			}
		}
	}
	if defectDiagnosticErrorStatus("query", context.Canceled) != "query_cancelled" || defectDiagnosticErrorStatus("query", context.DeadlineExceeded) != "query_deadline" {
		t.Fatal("context identity lost")
	}
	if defectDiagnosticErrorStatus("PRIVATE", diagnosticStateError{"42501"}) != "unavailable" {
		t.Fatal("untrusted label escaped")
	}
}

type diagnosticDeniedRow struct{}

func (diagnosticDeniedRow) Scan(dest ...any) error {
	if len(dest) > 0 {
		*dest[0].(*bool) = true
	}
	return diagnosticStateError{"42501"}
}

type diagnosticDeniedTx struct {
	pgx.Tx
	calls, rolled int
}

func (t *diagnosticDeniedTx) Begin(context.Context) (pgx.Tx, error) { return t, nil }
func (t *diagnosticDeniedTx) QueryRow(context.Context, string, ...any) pgx.Row {
	t.calls++
	if t.calls == 2 {
		return diagnosticDeniedRow{}
	}
	return closedDiagnosticRow{}
}
func (t *diagnosticDeniedTx) Rollback(context.Context) error { t.rolled++; return nil }
func TestDefectAdmissionDiagnosticDeniedCatalogHasUnknownFacts(t *testing.T) {
	tx := &diagnosticDeniedTx{}
	original := errors.New("refusal")
	got := defectAdmissionClosedDiagnostic(context.Background(), tx, uuid.New(), uuid.New(), "PRIVATE evidence", original)
	for _, part := range []string{"pure_status=observed", "closure=true", "roots=false", "catalog_status=query_permission_denied", "tenant_context=unknown", "scheduler=unknown", "draft=unknown", "one_task=unknown", "five_output_policies=unknown", "live_reservation=unknown"} {
		if !strings.Contains(got.Error(), part) {
			t.Fatal("missing truthful diagnostic", part)
		}
	}
	if tx.rolled != 2 || !errors.Is(got, original) || strings.Contains(got.Error(), "PRIVATE") {
		t.Fatal("original/restoration/privacy changed")
	}
}
