package v2region

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

type regionSourceRow struct{ bucket string }

func (r regionSourceRow) Scan(dest ...any) error {
	*dest[0].(*uuid.UUID) = uuid.New()
	*dest[1].(*uuid.UUID) = uuid.New()
	*dest[2].(*string) = regionSHA
	*dest[3].(*string) = r.bucket
	*dest[4].(*string) = "source"
	*dest[5].(*string) = "pinned-version"
	*dest[6].(*int64) = 123
	*dest[7].(*string) = "application/json"
	return nil
}

type regionSourceDB struct {
	bucket string
	args   []any
	query  string
}

func (d *regionSourceDB) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	d.args = args
	d.query = q
	return regionSourceRow{d.bucket}
}
func TestRegionSourceScopeAndBucketValidation(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	db := &regionSourceDB{bucket: "private"}
	metadata, err := ReadRegionSource(context.Background(), db, "private", ids[0], ids[1], ids[2], ids[3], ids[4])
	if err != nil || metadata.Object.VersionID != "pinned-version" || metadata.Object.SHA256 != regionSHA {
		t.Fatal("valid metadata rejected", err)
	}
	for i, id := range ids {
		if db.args[i] != id {
			t.Fatal("scope changed")
		}
	}
	db.bucket = "foreign"
	if _, err := ReadRegionSource(context.Background(), db, "private", ids[0], ids[1], ids[2], ids[3], ids[4]); err != ErrRegionInvalid {
		t.Fatal("foreign bucket accepted")
	}
	db.args = nil
	if _, err := ReadRegionSource(context.Background(), db, "private", uuid.Nil, ids[1], ids[2], ids[3], ids[4]); err != ErrRegionInvalid || db.args != nil {
		t.Fatal("invalid scope queried")
	}
}
