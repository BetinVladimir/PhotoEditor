package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

func TestDeliveryAssemblyParamsRepositoryPinsCurrentRootParams(t *testing.T) {
	db := &deliveryParamsDB{row: deliveryParamsRow{outcome: "created", id: uuid.New(), token: uuid.New(), accepted: true}}
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Tenant: uuid.New(), Run: uuid.New(), SlotCount: 1, Cursor: 1}
	r := deliveryAssemblyParamsRepository{db: db, bucket: "documents", lease: lease, purpose: "task_params", owner: uuid.New(), body: []byte(`{"schema_version":1}`)}
	scope := r.scope()
	sha := renderDerivativeBytesSHA(r.body)
	ticket, err := r.Reserve(context.Background(), scope, sha, int64(len(r.body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(db.args) != 7 || db.args[0] != lease.Expansion || db.args[1] != lease.Token || db.args[2] != string(r.body) || db.args[6] != r.owner {
		t.Fatal("reserve binding drift", db.args)
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	object := objectstore.Object{Key: key, VersionID: "version", SHA256: sha, Bytes: int64(len(r.body)), ContentType: "application/json"}
	if _, err := r.Complete(context.Background(), scope, ticket, object); err != nil {
		t.Fatal(err)
	}
	if len(db.args) != 10 || db.args[2] != string(r.body) || db.args[6] != "version" || db.args[9] != r.owner {
		t.Fatal("complete binding drift", db.args)
	}
	for _, scenario := range []string{"scope", "purpose", "cursor", "sha", "version", "key"} {
		t.Run(scenario, func(t *testing.T) {
			candidate := r
			currentScope := scope
			currentObject := object
			switch scenario {
			case "scope":
				currentScope.Run = uuid.New()
			case "purpose":
				candidate.purpose = "v30_delivery_assembly"
			case "cursor":
				candidate.lease.Cursor = 0
			case "sha":
				currentObject.SHA256 = "foreign"
			case "version":
				currentObject.VersionID = "null"
			case "key":
				currentObject.Key = "foreign"
			}
			before := db.calls
			if _, err := candidate.Complete(context.Background(), currentScope, ticket, currentObject); err == nil || db.calls != before {
				t.Fatal("invalid Params reached SQL", scenario)
			}
		})
	}
}
