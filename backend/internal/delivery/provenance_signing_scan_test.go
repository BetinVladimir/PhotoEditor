package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type signingScanDB struct {
	start *int
	calls int
	query string
	args []any
}

func (d *signingScanDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.calls++
	d.query = query
	d.args = append([]any(nil), args...)
	return signingScanRow{start: d.start}
}

type signingScanRow struct { start *int }
func (r signingScanRow) Scan(dest ...any) error {
	*dest[0].(**int) = r.start
	return nil
}

func TestSigningScanRequiresCurrentFullLease(t *testing.T) {
	value := 40
	lease := ExpansionLease{Expansion: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Minute), Tenant: uuid.New(), Run: uuid.New(),
		Workflow: uuid.New(), Package: uuid.New(), ParentTask: uuid.New(),
		Cursor: 80, SlotCount: 80}
	for _, mode := range []string{"valid", "nil", "foreign", "partial", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			db := &signingScanDB{start: &value}
			current := lease
			ctx := context.Background()
			switch mode {
			case "nil": db.start = nil
			case "foreign": bad := 80; db.start = &bad
			case "partial": current.Cursor = 79
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			start, err := (ExpansionLeaseStore{DB: db}).NextSigningPage(ctx,
				current, 40)
			if mode == "valid" {
				if err != nil || start != value || db.calls != 1 ||
					len(db.args) != 3 || db.args[0] != lease.Expansion ||
					db.args[1] != lease.Token || db.args[2] != 40 {
					t.Fatal(start, err, db)
				}
			} else if err == nil || (mode == "partial" || mode == "cancelled") && db.calls != 0 {
				t.Fatal("invalid scan accepted", start, err, db.calls)
			}
			if mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}
