package v2optical

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestMultiIntentIdentitySeparatesConflictHashFromStableRun(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		_, _, _, _, params, _ := multiQualifiedFixture(t, operation)
		who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
		tenant, project := uuid.New(), uuid.New()
		key := uuid.NewString()
		intent := MultiIntent{SchemaVersion: 1, Snapshot: uuid.New(), Params: params}
		first, err := compileMultiIntentIdentity(who, tenant, project, operation, key, intent)
		if err != nil {
			t.Fatal(err)
		}
		other := who
		other.SessionID = uuid.New()
		session, err := compileMultiIntentIdentity(other, tenant, project, operation, key, intent)
		if err != nil {
			t.Fatal(err)
		}
		if session.Run != first.Run || session.Route != first.Route || session.Hash == first.Hash {
			t.Fatal("session change must conflict under same resource")
		}
		intent.Snapshot = uuid.New()
		snapshot, err := compileMultiIntentIdentity(who, tenant, project, operation, key, intent)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Run != first.Run || snapshot.Hash == first.Hash {
			t.Fatal("snapshot change must conflict under same key")
		}
		changed, err := compileMultiIntentIdentity(who, tenant, uuid.New(), operation, key, intent)
		if err != nil {
			t.Fatal(err)
		}
		if changed.Run == first.Run || changed.Route == first.Route {
			t.Fatal("project scope collided")
		}
	}
}
