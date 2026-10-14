package delivery

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

type leaseRow struct {
	lease    ExpansionLease
	accepted bool
	err      error
}

func (r leaseRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 1 {
		*dest[0].(*bool) = r.accepted
		return nil
	}
	values := []uuid.UUID{r.lease.Expansion, r.lease.Token, r.lease.Tenant, r.lease.Run, r.lease.Workflow, r.lease.Package, r.lease.ParentTask}
	positions := []int{0, 1, 3, 4, 5, 6, 7}
	for i, pos := range positions {
		*dest[pos].(*uuid.UUID) = values[i]
	}
	*dest[2].(*time.Time) = r.lease.Until
	*dest[8].(*int) = r.lease.Cursor
	*dest[9].(*int) = r.lease.SlotCount
	return nil
}

type leaseDB struct {
	row   leaseRow
	args  []any
	calls int
	query string
}

func (d *leaseDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.calls++
	d.query = query
	d.args = args
	return d.row
}
func validExpansionLease() ExpansionLease {
	return ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute), Tenant: uuid.New(), Run: uuid.New(), Workflow: uuid.New(), Package: uuid.New(), ParentTask: uuid.New(), Cursor: 2, SlotCount: 10}
}
func TestExpansionLeaseClaimAndEmptyQueue(t *testing.T) {
	db := &leaseDB{row: leaseRow{lease: validExpansionLease()}}
	instance := uuid.New()
	store := ExpansionLeaseStore{DB: db, Instance: instance, Bucket: "photos"}
	lease, err := store.Claim(context.Background())
	if err != nil || lease == nil || *lease != db.row.lease {
		t.Fatal(lease, err)
	}
	if !strings.Contains(db.query, "claim_v30_delivery_expansion_bound") || len(db.args) != 2 || db.args[0] != instance || db.args[1] != "photos" {
		t.Fatal("delivery claim did not bind scheduler instance and bucket", db.query, db.args)
	}
	db.row.err = pgx.ErrNoRows
	lease, err = store.Claim(context.Background())
	if err != nil || lease != nil {
		t.Fatal("empty queue", lease, err)
	}
	db.row.err = errors.New("database unavailable")
	_, err = store.Claim(context.Background())
	if !errors.Is(err, db.row.err) {
		t.Fatal("lost database error")
	}
}
func TestExpansionLeaseRejectsInvalidDatabaseCursor(t *testing.T) {
	db := &leaseDB{row: leaseRow{lease: validExpansionLease()}}
	db.row.lease.Cursor = 11
	if _, err := (ExpansionLeaseStore{DB: db, Instance: uuid.New(), Bucket: "photos"}).Claim(context.Background()); err == nil {
		t.Fatal("invalid lease accepted")
	}
}

func TestExpansionLeaseClaimRequiresInstanceAndBucket(t *testing.T) {
	db := &leaseDB{row: leaseRow{lease: validExpansionLease()}}
	for _, store := range []ExpansionLeaseStore{
		{DB: db},
		{DB: db, Instance: uuid.New()},
		{DB: db, Instance: uuid.New(), Bucket: "x"},
	} {
		if _, err := store.Claim(context.Background()); err == nil || db.calls != 0 {
			t.Fatal("unbound delivery claim reached SQL", err, db.calls)
		}
	}
}
func TestExpansionLeaseRenewReleaseBindFenceAndRefuseStale(t *testing.T) {
	db := &leaseDB{row: leaseRow{accepted: true}}
	store := ExpansionLeaseStore{DB: db}
	lease := validExpansionLease()
	if err := store.Renew(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if len(db.args) != 3 || db.args[0] != lease.Expansion || db.args[1] != lease.Token || db.args[2] != false {
		t.Fatal("renew fence changed")
	}
	if err := store.Release(context.Background(), lease); err != nil || db.args[2] != true {
		t.Fatal("release binding", err)
	}
	db.row.accepted = false
	if err := store.Renew(context.Background(), lease); err == nil {
		t.Fatal("stale token accepted")
	}
	calls := db.calls
	lease.Token = uuid.Nil
	if err := store.Release(context.Background(), lease); err == nil || db.calls != calls {
		t.Fatal("invalid lease reached SQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Claim(ctx); err != context.Canceled || db.calls != calls {
		t.Fatal("cancelled claim reached SQL")
	}
}
