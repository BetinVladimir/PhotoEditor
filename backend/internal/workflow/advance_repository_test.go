package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"reflect"
	"strings"
	"testing"
)

func TestBoundTaskActivationCAS(t *testing.T) {
	a := BoundTaskActivation{TaskID: uuid.New(), ParamsID: uuid.New(), InputDigest: strings.Repeat("a", 64)}
	q := &expansionQueries{row: expansionRow{values: []any{true}}}
	r := AdvancementRepository{DB: q}
	ok, err := r.Activate(context.Background(), a)
	if err != nil || !ok || !reflect.DeepEqual(q.args, []any{a.TaskID, a.InputDigest, a.ParamsID}) {
		t.Fatal("binding CAS lost", err)
	}
	q.row = expansionRow{values: []any{false}}
	if ok, err = r.Activate(context.Background(), a); err != nil || ok {
		t.Fatal("ineligible treated as activated")
	}
	failure := errors.New("database down")
	q.row = expansionRow{err: failure}
	if ok, err = r.Activate(context.Background(), a); ok || !errors.Is(err, failure) {
		t.Fatal("DB failure lost")
	}
	a.InputDigest = "bad"
	q.args = nil
	if ok, err = r.Activate(context.Background(), a); ok || err == nil || q.args != nil {
		t.Fatal("invalid digest queried")
	}
}
