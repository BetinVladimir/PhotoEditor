package v2optical

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type multiClaimTx struct {
	perspectiveArrayTx
	outcome, body, key, operation string
	observed                      bool
}

func (t *multiClaimTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT current_user" {
		return t.negativeLockTx.QueryRow(ctx, q, args...)
	}
	return negativeLockRow(func(dest ...any) error {
		if q != "SELECT photo.claim_v29_multi_intent($1,$2,$3,$4,$5,$6,$7)" || len(args) != 7 || args[0] != t.tenant || args[4] != t.operation || args[5] != t.key || args[6] != t.body {
			return errors.New("claim lost canonical identity")
		}
		t.observed = true
		*dest[0].(*string) = t.outcome
		return nil
	})
}

type multiClaimDB struct{ tx *multiClaimTx }

func (d multiClaimDB) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }
func TestMultiIntentClaimCommitAndConflicts(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		for _, outcome := range []string{"claimed", "conflict", "stale", "unexpected"} {
			t.Run(operation+"/"+outcome, func(t *testing.T) {
				_, _, _, _, params, _ := multiQualifiedFixture(t, operation)
				intent := MultiIntent{SchemaVersion: 1, Snapshot: uuid.New(), Params: params}
				body, err := canonical.Marshal(intent)
				if err != nil {
					t.Fatal(err)
				}
				tenant := uuid.New()
				key := uuid.NewString()
				tx := &multiClaimTx{perspectiveArrayTx: perspectiveArrayTx{negativeLockTx: negativeLockTx{tenant: tenant}}, outcome: outcome, body: string(body), key: key, operation: operation}
				err = ClaimMultiIntent(context.Background(), multiClaimDB{tx}, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, uuid.New(), operation, key, intent)
				expected := map[string]error{"claimed": nil, "conflict": v2input.ErrConflict, "stale": v2input.ErrExpired, "unexpected": v2input.ErrInvalid}[outcome]
				if err != expected || !tx.observed || tx.committed != (outcome == "claimed") || !tx.rolledBack {
					t.Fatal("incorrect claim outcome", err)
				}
			})
		}
	}
}
