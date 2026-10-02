package review

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type testRow struct{ values []any }

func (r testRow) Scan(dest ...any) error {
	for i, v := range r.values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type reviewTx struct {
	pgx.Tx
	child                     *reviewTx
	proposal                  VerifiedProposal
	run, task                 uuid.UUID
	query                     int
	writes                    []string
	committed, rolledBack     bool
	attested, cancelled       bool
	failAudit                 bool
	missingContext, wrongBase bool
}

func (t *reviewTx) Begin(context.Context) (pgx.Tx, error) { return t.child, nil }
func (t *reviewTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.query++
	switch t.query {
	case 1:
		return testRow{[]any{t.run, "waiting_review", t.cancelled}}
	case 2:
		base := t.proposal.document.Image.ID
		if t.wrongBase {
			base = uuid.New()
		}
		resolved, report := uuid.New(), uuid.New()
		var resolvedPtr *uuid.UUID = &resolved
		if t.missingContext {
			resolvedPtr = nil
		}
		return testRow{[]any{int64(1), "pending", t.proposal.ref.ID, t.proposal.ref.SHA256, "defects", &t.task, &base, resolvedPtr, &report}}
	case 3:
		if len(args) != 11 || args[7] != t.proposal.document.Image.ID || args[8] != t.proposal.document.Image.SHA256 {
			panic("source image absent from attestation arguments")
		}
		return testRow{[]any{t.attested}}
	default:
		panic("unexpected query")
	}
}
func (t *reviewTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "audit_log") && t.failAudit {
		return pgconn.CommandTag{}, errors.New("audit failed")
	}
	t.writes = append(t.writes, sql)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (t *reviewTx) Commit(context.Context) error { t.committed = true; return nil }
func (t *reviewTx) Rollback(context.Context) error {
	if !t.committed {
		t.rolledBack = true
		t.writes = nil
	}
	return nil
}

type testGate struct {
	tx    pgx.Tx
	calls int
	fail  bool
}

func (g *testGate) ApplyDefectDecision(_ context.Context, tx pgx.Tx, s DefectSnapshot, p DefectPlan) error {
	g.tx = tx
	g.calls++
	if p.Route != "repair" {
		return errors.New("wrong route")
	}
	_, err := tx.Exec(context.Background(), "gate routing mutation")
	if g.fail {
		return errors.New("gate failed")
	}
	return err
}

func TestPersistenceSavepointBoundary(t *testing.T) {
	for _, scenario := range []string{"success", "gate failure", "audit failure", "unattested", "cancelled", "missing context", "wrong base"} {
		t.Run(scenario, func(t *testing.T) {
			raw, ref, source := proposalFixture(t)
			proposal, err := ReadDefectProposal(context.Background(), bytes.NewReader(raw), ref, source)
			if err != nil {
				t.Fatal(err)
			}
			child := &reviewTx{proposal: proposal, run: uuid.New(), task: uuid.New(), attested: scenario != "unattested", cancelled: scenario == "cancelled", failAudit: scenario == "audit failure", missingContext: scenario == "missing context", wrongBase: scenario == "wrong base"}
			outer := &reviewTx{child: child}
			gate := &testGate{fail: scenario == "gate failure"}
			request := Decision{ExpectedRevision: 1, CandidateHash: ref.SHA256, Decision: "accept", Accepted: []string{proposal.document.Candidates[0].ID}}
			plan, err := PersistDefectsAuthorized(context.Background(), outer, uuid.New(), uuid.New(), uuid.New(), uuid.New(), proposal, request, gate)
			if outer.committed || outer.rolledBack {
				t.Fatal("must not finish outer transaction")
			}
			if scenario == "success" {
				if err != nil || !child.committed || child.rolledBack || len(child.writes) != 3 || gate.tx != child || plan.Revision != 2 {
					t.Fatalf("incomplete success %+v %v", child, err)
				}
			} else {
				if err == nil || child.committed || !child.rolledBack || len(child.writes) != 0 || plan.DecisionHash != "" {
					t.Fatalf("partial mutation leaked: %+v %v", child, err)
				}
				if scenario != "gate failure" && gate.calls != 0 {
					t.Fatal("gate ran before prerequisites")
				}
			}
		})
	}
}

func TestPersistenceRequiresGate(t *testing.T) {
	if _, err := PersistDefectsAuthorized(context.Background(), &reviewTx{}, uuid.New(), uuid.New(), uuid.New(), uuid.New(), VerifiedProposal{}, Decision{}, nil); err == nil {
		t.Fatal("missing gate accepted")
	}
}
