package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
)

func TestBackgroundCleanupReviewCommitRejectsMissingTransaction(t *testing.T) {
	response, err := CommitShownBackgroundCleanup(context.Background(), nil, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "request", PreparedReview{}, nil, ApplyDecision{}, PublishedDecision{}, nil)
	if err != ErrInvalid || response.Status != 0 {
		t.Fatal("missing transaction accepted", err)
	}
}

// Embedding leaves unimplemented DB methods unusable: reaching IO fails the test.
type canceledCleanupCommitTX struct {
	pgx.Tx
	t *testing.T
}

func (tx canceledCleanupCommitTX) QueryRow(context.Context, string, ...any) pgx.Row {
	tx.t.Fatal("canceled commit queried database")
	return nil
}
func TestBackgroundCleanupReviewCommitCancellationBeforeValidationOrDatabase(t *testing.T) {
	for _, withTX := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var tx pgx.Tx
		if withTX {
			tx = canceledCleanupCommitTX{t: t}
		}
		response, err := CommitShownBackgroundCleanup(ctx, tx, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "request", PreparedReview{}, nil, ApplyDecision{}, PublishedDecision{}, nil)
		if err != context.Canceled || response.Status != 0 || response.Body != nil {
			t.Fatal("canceled commit escaped", err, response)
		}
	}
}

// Completed receipts must bypass resource/source/prepared-input callbacks.
// This recorder only supports identity and the receipt lock; other SQL fails.
type cleanupReplayRow func(...any) error

func (row cleanupReplayRow) Scan(dest ...any) error { return row(dest...) }

type cleanupReplayTX struct {
	pgx.Tx
	t             *testing.T
	identity      ReviewIdentity
	hash          string
	body          []byte
	batch         uuid.UUID
	reads, writes int
}

func (tx *cleanupReplayTX) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.writes++
	if !strings.Contains(sql, "set_config") && !strings.HasPrefix(sql, "INSERT INTO photo.idempotency_records") {
		tx.t.Fatalf("replay attempted mutation: %s", sql)
	}
	return pgconn.NewCommandTag("INSERT 0 0"), nil
}
func (tx *cleanupReplayTX) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.reads++
	switch {
	case strings.Contains(sql, "photo.semantic_review_identity"):
		return cleanupReplayRow(func(d ...any) error {
			*(d[0].(*uuid.UUID)) = tx.identity.RunID
			*(d[1].(*uuid.UUID)) = tx.identity.WorkflowID
			*(d[2].(*uuid.UUID)) = tx.identity.ProjectID
			return nil
		})
	case strings.Contains(sql, "FROM photo.idempotency_records"):
		return cleanupReplayRow(func(d ...any) error {
			*(d[0].(*uuid.UUID)) = uuid.New()
			*(d[1].(*string)) = tx.hash
			status := 200
			*(d[2].(**int)) = &status
			*(d[3].(*[]byte)) = append([]byte(nil), tx.body...)
			batch := tx.batch
			*(d[4].(**uuid.UUID)) = &batch
			return nil
		})
	default:
		tx.t.Fatalf("replay reached source/admission query: %s", sql)
		return nil
	}
}

func TestBackgroundCleanupCompletedReplayNeedsNoPreparedInputs(t *testing.T) {
	for _, mode := range []string{"valid", "wrong_workflow", "wrong_resource", "changed_request"} {
		t.Run(mode, func(t *testing.T) {
			identity := ReviewIdentity{RunID: uuid.New(), WorkflowID: uuid.New(), ProjectID: uuid.New()}
			request := ApplyDecision{ExpectedRevision: "1", PlanSHA: strings.Repeat("a", 64), TargetDecisions: []TargetDecision{{TargetID: uuid.New(), Decision: "approve"}}}
			hash, err := HashApplyRequest(identity.RunID, request)
			if err != nil {
				t.Fatal(err)
			}
			batch, workflow := uuid.New(), identity.WorkflowID
			if mode == "wrong_workflow" {
				workflow = uuid.New()
			}
			body, err := json.Marshal(map[string]any{"workflow_id": workflow, "revision": "2", "remaining": 0, "batch_id": batch, "child": map[string]any{"run_id": uuid.New(), "state": "queued"}})
			if err != nil {
				t.Fatal(err)
			}
			tx := &cleanupReplayTX{t: t, identity: identity, hash: hash, body: body, batch: batch}
			if mode == "wrong_resource" {
				tx.batch = uuid.New()
			}
			if mode == "changed_request" {
				request.ExpectedRevision = "2"
			}
			response, err := CommitShownBackgroundCleanup(context.Background(), tx, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "replay-key", PreparedReview{}, nil, request, PublishedDecision{}, nil)
			if mode == "valid" {
				if err != nil || !response.Replayed || response.Status != 200 || response.ResourceID == nil || *response.ResourceID != batch {
					t.Fatal("completed replay", response, err)
				}
			} else if err == nil || response.Status != 0 || response.Body != nil {
				t.Fatal("invalid replay escaped", response, err)
			}
			if tx.reads != 3 || tx.writes != 2 {
				t.Fatal("unexpected replay IO", tx.reads, tx.writes)
			}
		})
	}
}
