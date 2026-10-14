package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type deliveryParamsRow struct {
	outcome   string
	id, token uuid.UUID
	accepted  bool
}

func (r deliveryParamsRow) Scan(dest ...any) error {
	if len(dest) == 1 {
		*dest[0].(*bool) = r.accepted
		return nil
	}
	*dest[0].(*string) = r.outcome
	*dest[1].(**uuid.UUID) = &r.id
	*dest[2].(**uuid.UUID) = &r.token
	*dest[3].(*bool) = r.outcome == "replayed"
	return nil
}

type deliveryParamsDB struct {
	row   deliveryParamsRow
	calls int
	args  []any
}

func (d *deliveryParamsDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	d.calls++
	d.args = args
	return d.row
}

func TestDeliveryParamsRepositoryBindsParentScopeAndBothFences(t *testing.T) {
	db := &deliveryParamsDB{row: deliveryParamsRow{outcome: "created", id: uuid.New(), token: uuid.New(), accepted: true}}
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Tenant: uuid.New(), Run: uuid.New(), SlotCount: 1, Cursor: 0}
	r := deliveryParamsRepository{db: db, bucket: "documents", lease: lease, slot: 0, owner: uuid.New(), params: []byte(`{"schema_version":1}`)}
	scope := r.scope()
	sha := renderDerivativeBytesSHA(r.params)
	ticket, err := r.Reserve(context.Background(), scope, sha, int64(len(r.params)))
	if err != nil {
		t.Fatal(err)
	}
	if len(db.args) != 8 || db.args[0] != lease.Expansion || db.args[1] != lease.Token || db.args[5] != scope.Tenant || db.args[6] != scope.Run || db.args[7] != scope.Owner {
		t.Fatal("reservation scope drift")
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	object := objectstore.Object{Key: key, SHA256: sha, Bytes: int64(len(r.params)), ContentType: "application/json", VersionID: "version-1"}
	ref, err := r.Complete(context.Background(), scope, ticket, object)
	if err != nil || ref.ID != ticket.ArtifactID || ref.SHA256 != sha {
		t.Fatal(ref, err)
	}
	if len(db.args) != 11 || db.args[6] != ticket.Token || db.args[8] != scope.Tenant || db.args[9] != scope.Run || db.args[10] != scope.Owner {
		t.Fatal("completion scope drift")
	}
	calls := db.calls
	foreign := scope
	foreign.Run = uuid.New()
	if _, err := r.Reserve(context.Background(), foreign, sha, int64(len(r.params))); err == nil || db.calls != calls {
		t.Fatal("foreign scope reached SQL")
	}
	object.Key = "wrong"
	if _, err := r.Complete(context.Background(), scope, ticket, object); err == nil || db.calls != calls {
		t.Fatal("foreign object reached SQL")
	}
	for _, tc := range []struct {
		outcome  string
		expected error
	}{{"conflict", artifactjson.ErrConflict}, {"forbidden", artifactjson.ErrStale}, {"quota", artifactjson.ErrQuota}} {
		db.row.outcome = tc.outcome
		if _, err := r.Reserve(context.Background(), scope, sha, int64(len(r.params))); !errors.Is(err, tc.expected) {
			t.Fatal(tc.outcome, err)
		}
	}
	db.row.outcome = "replayed"
	replay, err := r.Reserve(context.Background(), scope, sha, int64(len(r.params)))
	if err != nil || replay.Ready == nil || replay.Ready.ID != ticket.ArtifactID {
		t.Fatal("ready replay", err)
	}
}
