package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

func TestDeliveryShardRepositoryPinsScopeBodyAndObjectVersion(t *testing.T) {
	db := &deliveryParamsDB{row: deliveryParamsRow{outcome: "created", id: uuid.New(), token: uuid.New(), accepted: true}}
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Tenant: uuid.New(), Run: uuid.New(), SlotCount: 501, Cursor: 501}
	repository := deliveryShardRepository{db: db, bucket: "documents", lease: lease, purpose: "v30_delivery_slots_001", owner: uuid.New(), body: []byte(`{"schema_version":1}`)}
	scope := repository.scope()
	sha := renderDerivativeBytesSHA(repository.body)
	ticket, err := repository.Reserve(context.Background(), scope, sha, int64(len(repository.body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(db.args) != 8 || db.args[0] != lease.Expansion || db.args[1] != lease.Token || db.args[2] != repository.purpose || db.args[3] != string(repository.body) || db.args[7] != repository.owner {
		t.Fatal("reserve binding drift", db.args)
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	object := objectstore.Object{Key: key, VersionID: "exact-version", SHA256: sha, Bytes: int64(len(repository.body)), ContentType: "application/json"}
	ref, err := repository.Complete(context.Background(), scope, ticket, object)
	if err != nil || ref.ID != ticket.ArtifactID {
		t.Fatal(ref, err)
	}
	if len(db.args) != 11 || db.args[2] != repository.purpose || db.args[7] != object.VersionID {
		t.Fatal("complete binding drift")
	}
	for _, scenario := range []string{"purpose", "scope", "sha", "version", "key", "cursor"} {
		t.Run(scenario, func(t *testing.T) {
			candidate := repository
			currentScope := scope
			currentObject := object
			switch scenario {
			case "purpose":
				candidate.purpose = "v30_delivery_assembly"
			case "scope":
				currentScope.Tenant = uuid.New()
			case "sha":
				currentObject.SHA256 = "foreign"
			case "version":
				currentObject.VersionID = "null"
			case "key":
				currentObject.Key = "foreign"
			case "cursor":
				candidate.lease.Cursor--
			}
			before := db.calls
			if _, err := candidate.Complete(context.Background(), currentScope, ticket, currentObject); err == nil || db.calls != before {
				t.Fatal("invalid completion reached SQL", scenario)
			}
		})
	}
}

func TestDeliveryShardPurposeMatchesActualShardCount(t *testing.T) {
	for _, test := range []struct {
		purpose string
		slots   int
		valid   bool
	}{
		{"v30_delivery_slots_000", 1, true}, {"v30_delivery_slots_001", 500, false},
		{"v30_delivery_slots_001", 501, true}, {"v30_delivery_slots_159", 80000, true},
		{"v30_delivery_slots_160", 80000, false}, {"v30_delivery_slots_01", 501, false},
		{"v30_delivery_assembly", 1, false},
	} {
		if validDeliveryShardPurpose(test.purpose, test.slots) != test.valid {
			t.Fatal(test)
		}
	}
}

func TestDeliveryAssemblyRepositoryPinsExactShardProof(t *testing.T) {
	db := &deliveryParamsDB{row: deliveryParamsRow{outcome: "created", id: uuid.New(), token: uuid.New(), accepted: true}}
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Tenant: uuid.New(), Run: uuid.New(), SlotCount: 1, Cursor: 1}
	repository := deliveryShardRepository{db: db, bucket: "documents", lease: lease, purpose: "v30_delivery_assembly", owner: uuid.New(), body: []byte(`{"schema_version":1}`), assemblyProof: []byte(`["exact shard bytes"]`)}
	scope := repository.scope()
	sha := renderDerivativeBytesSHA(repository.body)
	ticket, err := repository.Reserve(context.Background(), scope, sha, int64(len(repository.body)))
	if err != nil {
		t.Fatal(err)
	}
	if db.args[2] != string(repository.body) || db.args[3] != string(repository.assemblyProof) {
		t.Fatal("root proof lost", db.args)
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	object := objectstore.Object{Key: key, VersionID: "version", SHA256: sha, Bytes: int64(len(repository.body)), ContentType: "application/json"}
	if _, err := repository.Complete(context.Background(), scope, ticket, object); err != nil {
		t.Fatal(err)
	}
	if db.args[2] != string(repository.body) || db.args[3] != string(repository.assemblyProof) || db.args[7] != "version" {
		t.Fatal("root completion proof lost", db.args)
	}
	for _, purpose := range []string{"v30_delivery_slots_000", "task_params", "unknown"} {
		candidate := repository
		candidate.purpose = purpose
		before := db.calls
		if _, err := candidate.Reserve(context.Background(), candidate.scope(), sha, int64(len(candidate.body))); err == nil || db.calls != before {
			t.Fatal("root proof crossed purpose", purpose)
		}
	}
	repository.assemblyProof = nil
	before := db.calls
	if _, err := repository.Reserve(context.Background(), scope, sha, int64(len(repository.body))); err == nil || db.calls != before {
		t.Fatal("root without proof reached SQL")
	}
}
