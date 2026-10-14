package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/objectstore"
)

func TestSignedDeliveryDocumentRepositoryFencesPublication(t *testing.T) {
	db := &deliveryParamsDB{row: deliveryParamsRow{outcome: "created", id: uuid.New(), token: uuid.New(), accepted: true}}
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute),
		Tenant: uuid.New(), Run: uuid.New(), Workflow: uuid.New(), Package: uuid.New(), ParentTask: uuid.New(), Cursor: 1, SlotCount: 1}
	r := signedDeliveryDocumentRepository{db: db, bucket: "documents", lease: lease, owner: uuid.New(),
		purpose: "v30_delivery_signed_closure", body: []byte(`{"schema_version":1}`)}
	scope := r.scope()
	sha := renderDerivativeBytesSHA(r.body)
	ticket, err := r.Reserve(context.Background(), scope, sha, int64(len(r.body)))
	if err != nil || ticket.Ready != nil { t.Fatal(ticket, err) }
	if len(db.args) != 8 || db.args[0] != lease.Expansion || db.args[1] != lease.Token ||
		db.args[2] != r.purpose || db.args[3] != string(r.body) || db.args[5] != scope.Tenant ||
		db.args[6] != scope.Run || db.args[7] != scope.Owner { t.Fatal("reserve scope drift") }
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil { t.Fatal(err) }
	object := objectstore.Object{Key: key, SHA256: sha, Bytes: int64(len(r.body)), ContentType: "application/json", VersionID: "version-1"}
	ref, err := r.Complete(context.Background(), scope, ticket, object)
	if err != nil || ref.ID != ticket.ArtifactID || ref.SHA256 != sha { t.Fatal(ref, err) }
	if len(db.args) != 11 || db.args[0] != lease.Expansion || db.args[1] != lease.Token ||
		db.args[5] != ticket.ArtifactID || db.args[6] != ticket.Token || db.args[7] != object.VersionID ||
		db.args[8] != scope.Tenant || db.args[9] != scope.Run || db.args[10] != scope.Owner { t.Fatal("complete scope drift") }
	calls := db.calls
	foreign := scope
	foreign.Run = uuid.New()
	if _, err := r.Reserve(context.Background(), foreign, sha, int64(len(r.body))); err == nil || db.calls != calls { t.Fatal("foreign scope reached SQL") }
	if _, err := r.Reserve(context.Background(), scope, "changed", int64(len(r.body))); err == nil || db.calls != calls { t.Fatal("changed digest reached SQL") }
	object.VersionID = "null"
	if _, err := r.Complete(context.Background(), scope, ticket, object); err == nil || db.calls != calls { t.Fatal("unversioned object reached SQL") }
	object.VersionID = "version-1"
	object.Key = "foreign"
	if _, err := r.Complete(context.Background(), scope, ticket, object); err == nil || db.calls != calls { t.Fatal("foreign object reached SQL") }
	for _, tc := range []struct{ outcome string; expected error }{
		{"conflict", artifactjson.ErrConflict}, {"stale", artifactjson.ErrStale}, {"forbidden", artifactjson.ErrStale}, {"quota", artifactjson.ErrQuota},
	} {
		db.row.outcome = tc.outcome
		if _, err := r.Reserve(context.Background(), scope, sha, int64(len(r.body))); !errors.Is(err, tc.expected) { t.Fatal(tc.outcome, err) }
	}
	db.row.outcome = "replayed"
	replayed, err := r.Reserve(context.Background(), scope, sha, int64(len(r.body)))
	if err != nil || replayed.Ready == nil || replayed.Ready.ID != ticket.ArtifactID || replayed.Ready.SHA256 != sha { t.Fatal(replayed, err) }
}

func TestSignedDeliveryDocumentRepositoryRejectsInvalidPurposeAndLease(t *testing.T) {
	db := &deliveryParamsDB{row: deliveryParamsRow{outcome: "created", id: uuid.New(), token: uuid.New()}}
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute),
		Tenant: uuid.New(), Run: uuid.New(), Workflow: uuid.New(), Package: uuid.New(), ParentTask: uuid.New(), Cursor: 1, SlotCount: 1}
	r := signedDeliveryDocumentRepository{db: db, bucket: "documents", lease: lease, owner: uuid.New(), body: []byte(`{}`)}
	for _, purpose := range []string{"task_params", "v30_delivery_signed_closure", "v30_delivery_signed_envelope"} {
		r.purpose = purpose
		_, err := r.Reserve(context.Background(), r.scope(), renderDerivativeBytesSHA(r.body), int64(len(r.body)))
		if (err == nil) != (purpose != "task_params") { t.Fatal(purpose, err) }
	}
	r.purpose = "v30_delivery_signed_envelope"
	r.body = make([]byte, 65537)
	if _, err := r.Reserve(context.Background(), r.scope(), renderDerivativeBytesSHA(r.body), int64(len(r.body))); err == nil { t.Fatal("oversize envelope") }
	r.body = []byte(`{}`)
	r.lease.Cursor = 0
	if _, err := r.Reserve(context.Background(), r.scope(), renderDerivativeBytesSHA(r.body), int64(len(r.body))); err == nil { t.Fatal("incomplete expansion") }
}
