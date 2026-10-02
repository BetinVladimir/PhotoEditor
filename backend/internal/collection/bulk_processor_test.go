package collection

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type bulkRow struct {
	value bool
	err   error
}

func (r bulkRow) Scan(values ...any) error {
	if r.err != nil {
		return r.err
	}
	*(values[0].(*bool)) = r.value
	return nil
}

type bulkDB struct {
	row   bulkRow
	query string
	args  []any
}

func (d *bulkDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.query = query
	d.args = args
	return d.row
}

func TestBulkProcessorCallsAtomicFunction(t *testing.T) {
	db := &bulkDB{row: bulkRow{value: true}}
	changed, err := (BulkProcessor{DB: db}).ProcessOne(context.Background())
	if err != nil || !changed || db.query != "SELECT photo.enqueue_bulk_chunk()" {
		t.Fatalf("%v %v %q", changed, err, db.query)
	}
	db.row = bulkRow{err: errors.New("db")}
	if _, err = (BulkProcessor{DB: db}).ProcessOne(context.Background()); err == nil {
		t.Fatal("database failure hidden")
	}
}
