package v2draft

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/v2input"
)

type restorationReceiptDB struct{ tx *restorationReceiptTx }

func (d restorationReceiptDB) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }

type restorationReceiptTx struct {
	pgx.Tx
	queries []string
	review  uuid.UUID
	commits int
}

func (t *restorationReceiptTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if !strings.Contains(sql, "set_config('app.tenant_id'") {
		return pgconn.CommandTag{}, errors.New("unexpected execution")
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (t *restorationReceiptTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	t.queries = append(t.queries, sql)
	switch len(t.queries) {
	case 1:
		return restorationReceiptRow{value: "photo_scheduler"}
	case 2:
		return restorationReceiptRow{value: true}
	case 3:
		if t.review == uuid.Nil {
			return restorationReceiptRow{err: pgx.ErrNoRows}
		}
		return restorationReceiptRow{value: t.review}
	}
	return restorationReceiptRow{err: errors.New("unexpected query")}
}

func (t *restorationReceiptTx) Commit(context.Context) error   { t.commits++; return nil }
func (t *restorationReceiptTx) Rollback(context.Context) error { return nil }

type restorationReceiptRow struct {
	value any
	err   error
}

func (r restorationReceiptRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("scan arity")
	}
	switch out := dest[0].(type) {
	case *string:
		*out = r.value.(string)
	case *bool:
		*out = r.value.(bool)
	case *uuid.UUID:
		*out = r.value.(uuid.UUID)
	default:
		return errors.New("scan type")
	}
	return nil
}

func TestRestorationReceiptAndReviewShareOneTransaction(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(),
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	evidence := RestorationVerificationEvidence{verified: true, Bucket: "photos",
		Scope: RestorationArrayScope{Tenant: uuid.New(), Task: uuid.New(),
			Result: uuid.New(), Root: ref.ArtifactID, RootSHA: ref.SHA256},
		Candidate: RestorationReviewCandidate{Result: ref},
		Objects:   make([]RestorationVerifiedObject, 10)}
	for _, review := range []uuid.UUID{uuid.Nil, uuid.New()} {
		tx := &restorationReceiptTx{review: review}
		err := (SQLRestorationAdoptionStore{DB: restorationReceiptDB{tx}}).Commit(
			context.Background(), evidence)
		if len(tx.queries) != 3 || !strings.Contains(tx.queries[1],
			"adopt_v28_restoration_verified") || !strings.Contains(tx.queries[2],
			"create_v28_restoration_review") {
			t.Fatal("receipt/review order lost", tx.queries)
		}
		if review == uuid.Nil && (err == nil || tx.commits != 0) {
			t.Fatal("receipt committed without review", err)
		}
		if review != uuid.Nil && (err != nil || tx.commits != 1) {
			t.Fatal("atomic receipt and review rejected", err)
		}
	}
}
