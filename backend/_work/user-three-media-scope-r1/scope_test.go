package mediascope

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	retryscope "photoeditor/backend/_work/user-raw-retry-scope-r3"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/workflow"
	"testing"
)

type row struct {
	event, tenant, version uuid.UUID
	err                    error
	scanned                *bool
}

func (r row) Scan(d ...any) error {
	if r.scanned != nil {
		*r.scanned = true
	}
	if r.err != nil {
		return r.err
	}
	*d[0].(*uuid.UUID) = r.event
	*d[2].(*uuid.UUID) = r.tenant
	*d[5].(*uuid.UUID) = r.version
	return nil
}

type querier struct {
	row     pgx.Row
	queries int
}

func (q *querier) QueryRow(context.Context, string, ...any) pgx.Row { q.queries++; return q.row }
func validScope() (Scope, Binding) {
	s := Scope{}
	var chosen Binding
	for i := 0; i < 3; i++ {
		b := Binding{uuid.New(), uuid.New(), uuid.New()}
		s[b.EventID] = b
		chosen = b
	}
	return s, chosen
}
func destinations() []any {
	d := make([]any, 17)
	for i := range d {
		d[i] = new(uuid.UUID)
	}
	return d
}
func TestGuardRowAcceptsOnlyExactEventTenantVersion(t *testing.T) {
	for _, mode := range []string{"exact", "event", "tenant", "version"} {
		t.Run(mode, func(t *testing.T) {
			s, b := validScope()
			r := row{event: b.EventID, tenant: b.TenantID, version: b.VersionID}
			switch mode {
			case "event":
				r.event = uuid.New()
			case "tenant":
				r.tenant = uuid.New()
			case "version":
				r.version = uuid.New()
			}
			q := &querier{row: r}
			g := GuardedClaims{DB: q, Scope: s}
			err := g.QueryRow(context.Background(), ClaimQuery).Scan(destinations()...)
			if (mode == "exact") != (err == nil) {
				t.Fatal("scope guard mismatch")
			}
		})
	}
}
func TestGuardForeignStopsBeforeObjectStage(t *testing.T) {
	s, b := validScope()
	q := &querier{row: row{uuid.New(), b.TenantID, b.VersionID, nil, nil}}
	stage := false
	err := (GuardedClaims{DB: q, Scope: s}).QueryRow(context.Background(), ClaimQuery).Scan(destinations()...)
	if err == nil {
		stage = true
	}
	if !errors.Is(err, ErrOutsideScope) || stage {
		t.Fatal("foreign reached object stage")
	}
}
func TestGuardMalformedDestinationDoesNotReadRow(t *testing.T) {
	s, b := validScope()
	scanned := false
	r := guardedRow{row{b.EventID, b.TenantID, b.VersionID, nil, &scanned}, s}
	if r.Scan(new(uuid.UUID)) == nil || scanned {
		t.Fatal("malformed scan not rejected before read")
	}
}
func TestGuardNoRowPreserved(t *testing.T) {
	s, _ := validScope()
	r := guardedRow{row{err: pgx.ErrNoRows}, s}
	if !errors.Is(r.Scan(destinations()...), pgx.ErrNoRows) {
		t.Fatal("no-row semantics changed")
	}
}
func TestScopeRejectsDuplicatesAndWrongCardinality(t *testing.T) {
	s, b := validScope()
	var bs []Binding
	for _, v := range s {
		bs = append(bs, v)
	}
	if _, e := NewScope(bs); e != nil {
		t.Fatal(e)
	}
	if _, e := NewScope(bs[:2]); e == nil {
		t.Fatal("short scope")
	}
	bs[0] = b
	bs[1] = b
	if _, e := NewScope(bs); e == nil {
		t.Fatal("duplicate scope")
	}
}

type tx struct {
	q                  *querier
	commits, rollbacks int
}

func (t *tx) QueryRow(c context.Context, s string, a ...any) pgx.Row { return t.q.QueryRow(c, s, a...) }
func (t *tx) Commit(context.Context) error                           { t.commits++; return nil }
func (t *tx) Rollback(context.Context) error                         { t.rollbacks++; return nil }

type objects struct{ reads int }

func (o *objects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	o.reads++
	return nil, errors.New("unexpected object read")
}
func TestActualMediaStoreForeignClaimRollsBackBeforePolicyOrObjectRead(t *testing.T) {
	s, b := validScope()
	q := &querier{row: row{uuid.New(), b.TenantID, b.VersionID, nil, nil}}
	transaction := &tx{q: q}
	obj := &objects{}
	store := Store{MediaRetryStore: workflow.MediaRetryStore{Objects: obj, Bucket: "photos"}, Scope: s, BeginClaimTx: func(context.Context) (retryscope.Transaction, error) { return transaction, nil }}
	lease, err := store.Claim(context.Background())
	if lease != nil || !errors.Is(err, ErrOutsideScope) || transaction.commits != 0 || transaction.rollbacks != 1 || obj.reads != 0 || q.queries != 1 {
		t.Fatal("foreign claim escaped transaction guard or reached policy/object stage")
	}
}
func TestChangedClaimQueryRejectedBeforeCallingDatabase(t *testing.T) {
	s, _ := validScope()
	q := &querier{}
	err := (GuardedClaims{DB: q, Scope: s}).QueryRow(context.Background(), "SELECT * FROM photo.claim_media_retry_job()").Scan(destinations()...)
	if !errors.Is(err, ErrOutsideScope) || q.queries != 0 {
		t.Fatal("changed production query bypassed guard")
	}
}
