package v2qualification

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

type operatorChildDB struct {
	calls int
	ok    bool
	err   error
	args  []any
}
type operatorChildRow struct{ db *operatorChildDB }

func (r operatorChildRow) Scan(out ...any) error {
	if r.db.err != nil {
		return r.db.err
	}
	*out[0].(*bool) = r.db.ok
	return nil
}
func (d *operatorChildDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	d.calls++
	d.args = args
	return operatorChildRow{d}
}

// Synthetic readback tests constructor/binding only; no live qualification claimed.
func TestOperatorChildrenDoNotInventParentAdmission(t *testing.T) {
	a, e, _ := mixedFixture(t)
	run, actor := uuid.New(), uuid.New()
	db := &operatorChildDB{ok: true}
	port, err := ResolveOperatorMixedChildren(context.Background(), db, a.Tenant, a.Project, actor, run, e)
	if err != nil {
		t.Fatal(err)
	}
	if db.calls != 1 || len(db.args) != 6 {
		t.Fatal("readback omitted")
	}
	profile := []byte(`{"backend":"cpu"}`)
	if _, err = port.QualifyProfile(a.Tenant, run, e.Common.DatasetSHA, "render.compose", profile); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		tenant, run uuid.UUID
		corpus      string
	}{{uuid.New(), run, e.Common.DatasetSHA}, {a.Tenant, uuid.New(), e.Common.DatasetSHA}, {a.Tenant, run, ""}} {
		if _, err = port.QualifyProfile(bad.tenant, bad.run, bad.corpus, "render.compose", profile); err == nil {
			t.Fatal("scope substitution accepted")
		}
	}
	// Caller mutation after construction cannot change owned routes.
	e.Routes["render.compose"] = "mps"
	if _, err = port.QualifyProfile(a.Tenant, run, e.Common.DatasetSHA, "render.compose", profile); err != nil {
		t.Fatal("borrowed mutable evidence")
	}
}
func TestOperatorChildrenRequireCurrentAuthorization(t *testing.T) {
	a, e, _ := mixedFixture(t)
	sentinel := errors.New("readback denied")
	for _, db := range []*operatorChildDB{{ok: false}, {err: sentinel}} {
		port, err := ResolveOperatorMixedChildren(context.Background(), db, a.Tenant, a.Project, uuid.New(), uuid.New(), e)
		if port != nil || err == nil {
			t.Fatal("no current authority accepted")
		}
		if db.err != nil && !errors.Is(err, sentinel) {
			t.Fatal("lost cause")
		}
	}
}
