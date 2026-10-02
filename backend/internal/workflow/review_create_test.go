package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"reflect"
	"testing"
)

func TestDefectReviewCreatorBindsCompilerContext(t *testing.T) {
	in := DefectReviewContext{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	id := uuid.New()
	q := &expansionQueries{row: expansionRow{values: []any{&id}}}
	got, err := (DefectReviewCreator{DB: q}).Create(context.Background(), in)
	if err != nil || got != id || !reflect.DeepEqual(q.args, []any{in.TenantID, in.RunID, in.TaskID, in.ImageID, in.ResolvedID}) {
		t.Fatalf("context mismatch: %v", err)
	}
	var missing *uuid.UUID
	q.row = expansionRow{values: []any{missing}}
	if got, err = (DefectReviewCreator{DB: q}).Create(context.Background(), in); err == nil || got != uuid.Nil {
		t.Fatal("conflicting context accepted")
	}
	sentinel := errors.New("database unavailable")
	q.row = expansionRow{err: sentinel}
	if _, err = (DefectReviewCreator{DB: q}).Create(context.Background(), in); !errors.Is(err, sentinel) {
		t.Fatal("database error lost")
	}
	q.args = nil
	in.ResolvedID = uuid.Nil
	if _, err = (DefectReviewCreator{DB: q}).Create(context.Background(), in); err == nil || q.args != nil {
		t.Fatal("incomplete context queried")
	}
}
