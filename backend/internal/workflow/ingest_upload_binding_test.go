package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestIngestUploadBinderRequiresExplicitUploadScope(t *testing.T) {
	tenant, run, upload := uuid.New(), uuid.New(), uuid.New()
	ids := IngestUploadBinding{uuid.New(), uuid.New(), uuid.New()}
	db := &expansionQueries{row: expansionRow{values: []any{ids.AssetID, ids.VersionID, ids.ItemID}}}
	binder := IngestUploadBinder{DB: db}
	got, err := binder.Bind(context.Background(), tenant, run, 9, upload)
	if err != nil || got != ids || len(db.args) != 4 || db.args[0] != tenant || db.args[1] != run || db.args[2] != int64(9) || db.args[3] != upload {
		t.Fatal("binding scope lost", err)
	}
	db.row = expansionRow{err: pgx.ErrNoRows}
	if got, err = binder.Bind(context.Background(), tenant, run, 9, upload); err == nil || got.AssetID != uuid.Nil {
		t.Fatal("missing binding accepted")
	}
}
func TestIngestUploadBinderRejectsInvalidIdentity(t *testing.T) {
	id := uuid.New()
	db := &expansionQueries{row: expansionRow{values: []any{id, id, uuid.New()}}}
	if _, err := (IngestUploadBinder{DB: db}).Bind(context.Background(), uuid.New(), uuid.New(), 0, uuid.New()); err == nil {
		t.Fatal("aliased catalog identities accepted")
	}
}
