package v2draft

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type restorationQueueRow struct {
	values []any
	err    error
}

func (r restorationQueueRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("scan count")
	}
	for i, value := range r.values {
		switch to := dest[i].(type) {
		case *uuid.UUID:
			*to = value.(uuid.UUID)
		case *string:
			*to = value.(string)
		default:
			return errors.New("scan type")
		}
	}
	return nil
}

type restorationQueueDB struct{ row pgx.Row }

func (q restorationQueueDB) QueryRow(context.Context, string, ...any) pgx.Row { return q.row }

func TestRestorationAdoptionQueueBindsDescriptorToCanonicalResult(t *testing.T) {
	tenant, run, task, project, snapshot := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	actor, session, result, root := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	params, err := canonical.Marshal(map[string]any{"schema_version": 1})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := canonical.Marshal(restorationTaskDescriptor{SchemaVersion: 1,
		Task: v2input.DraftTask{ID: task, Operation: "image.reflection", OperationVersion: "1",
			ResourceClass: "gpu"}, Params: params})
	if err != nil {
		t.Fatal(err)
	}
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	values := []any{tenant, run, task, project, snapshot, actor, session,
		"image.reflection", string(plan), result, root, sha}
	queue := SQLRestorationAdoptionQueue{DB: restorationQueueDB{row: restorationQueueRow{values: values}}}
	got, err := queue.Next(context.Background(), uuid.Nil)
	if err != nil || got == nil || got.Scope.Task != task || got.Scope.Result != result ||
		got.Scope.Root != root || got.Actor.UserID != actor || string(got.Params) != string(params) {
		t.Fatal("exact queued candidate refused", err)
	}
	values[7] = "portrait.glare"
	if got, err = queue.Next(context.Background(), uuid.Nil); err == nil || got != nil {
		t.Fatal("operation/descriptor mismatch accepted")
	}
	empty := SQLRestorationAdoptionQueue{DB: restorationQueueDB{row: restorationQueueRow{err: pgx.ErrNoRows}}}
	if got, err = empty.Next(context.Background(), uuid.Nil); err != nil || got != nil {
		t.Fatal("empty candidate queue not recognized", err)
	}
}
