package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

type gateTx struct {
	pgx.Tx
	args []any
	sql  string
	rows int64
	err  error
}

func (t *gateTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.sql = sql
	t.args = args
	if t.err != nil {
		return pgconn.CommandTag{}, t.err
	}
	if t.rows == 0 {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func gatePlan(route string) (review.DefectSnapshot, review.DefectPlan) {
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	s := review.DefectSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), Revision: 1, Proposal: ref, Image: ref, LabelMap: ref}
	selection := review.DefectSelection{Version: "defect_review_v1", Proposal: ref, Image: ref, LabelMap: ref, Revision: 2, Accepted: []string{}, Rejected: []string{}}
	if route == "repair" {
		selection.Accepted = []string{strings.Repeat("b", 64)}
	}
	return s, review.DefectPlan{Revision: 2, Route: route, Selection: selection, DecisionHash: strings.Repeat("c", 64)}
}
func TestReviewExpansionBranchBarrier(t *testing.T) {
	for _, route := range []string{"repair", "base", "skip"} {
		t.Run(route, func(t *testing.T) {
			s, p := gatePlan(route)
			tx := &gateTx{rows: 1}
			if err := (ReviewExpansionGate{}).ApplyDefectDecision(context.Background(), tx, s, p); err != nil {
				t.Fatal(err)
			}
			expected := 0
			if route == "repair" {
				expected = 1
			}
			if len(tx.args) != 8 || tx.args[7] != expected || tx.args[6] != "review.defects/"+s.ReviewID.String() || tx.args[4] != p.DecisionHash || tx.args[5] != route {
				t.Fatal("wrong expansion binding", tx.args)
			}
			if !strings.Contains(tx.sql, "graph_sealed=false") || !strings.Contains(tx.sql, "'open'") {
				t.Fatal("must leave graph unsealed until compiler completion")
			}
		})
	}
}
func TestReviewExpansionRefusesInvalidPlanAndStaleDB(t *testing.T) {
	for _, change := range []func(*review.DefectPlan){
		func(p *review.DefectPlan) { p.Route = "unknown" },
		func(p *review.DefectPlan) { p.Revision++ },
		func(p *review.DefectPlan) { p.Selection.Proposal.ID = uuid.New() },
		func(p *review.DefectPlan) { p.Selection.Accepted = nil },
		func(p *review.DefectPlan) { p.DecisionHash = "invalid" },
	} {
		s, p := gatePlan("repair")
		change(&p)
		tx := &gateTx{rows: 1}
		if err := (ReviewExpansionGate{}).ApplyDefectDecision(context.Background(), tx, s, p); err == nil || len(tx.args) != 0 {
			t.Fatal("invalid plan reached storage")
		}
	}
	s, p := gatePlan("repair")
	tx := &gateTx{}
	err := (ReviewExpansionGate{}).ApplyDefectDecision(context.Background(), tx, s, p)
	if err == nil || domain.AsError(err).Code != "REVISION_CONFLICT" {
		t.Fatal("missing decision treated as success", err)
	}
	sentinel := errors.New("database error")
	tx = &gateTx{err: sentinel}
	if err = (ReviewExpansionGate{}).ApplyDefectDecision(context.Background(), tx, s, p); !errors.Is(err, sentinel) {
		t.Fatal("swallowed storage error", err)
	}
}
