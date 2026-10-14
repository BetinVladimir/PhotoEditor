package artifactjson

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

type donorRouteDB struct {
	query    string
	args     []any
	complete bool
}

func (d *donorRouteDB) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	d.query = q
	d.args = args
	return donorRouteRow{d.complete}
}

type donorRouteRow struct{ complete bool }

func (r donorRouteRow) Scan(dest ...any) error {
	if r.complete {
		*(dest[0].(*bool)) = false
		return nil
	}
	*(dest[0].(*string)) = "forbidden"
	return nil
}
func TestDonorRepositoryRoutesAndPropagatesRevocation(t *testing.T) {
	ctx := context.Background()
	db := &donorRouteDB{}
	repo := SQLRepository{DB: db, Bucket: "photos"}
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", DonorPortrait: true}
	digest := strings.Repeat("a", 64)
	if _, err := repo.Reserve(ctx, s, digest, 2); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if !strings.Contains(db.query, "photo.reserve_v28_donor_params_document(") || len(db.args) != 7 || db.args[2] != s.Owner {
		t.Fatal("wrong reserve authority")
	}
	db.complete = true
	ticket := Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, err := DocumentKey(s, ticket.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	obj := objectstore.Object{Key: key, VersionID: "v1", SHA256: digest, Bytes: 2, ContentType: "application/json"}
	if _, err := repo.Complete(ctx, s, ticket, obj); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if !strings.Contains(db.query, "photo.complete_v28_donor_params_document(") || len(db.args) != 10 || db.args[2] != s.Owner {
		t.Fatal("wrong completion authority")
	}
	db.query = ""
	s.LocalPortrait = true
	if _, err := repo.Reserve(ctx, s, digest, 2); err == nil || db.query != "" {
		t.Fatal("mixed authority reached database")
	}
}
