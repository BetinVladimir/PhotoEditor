package v2optical

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestPerspectiveIntentStrictGeometryOnly(t *testing.T) {
	p := perspectiveIntent()
	raw, err := canonical.Marshal(PerspectiveIntent{SchemaVersion: 1, Snapshot: uuid.New(), Params: p})
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"", "source", "runtime", "missing", "snapshot", "unknown_param"} {
		var wire map[string]any
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "source":
			wire["source_jcs"] = "{}"
		case "runtime":
			wire["qualification"] = map[string]any{}
		case "missing":
			delete(wire, "schema_version")
		case "snapshot":
			wire["snapshot_id"] = uuid.Nil
		case "unknown_param":
			wire["params"].(map[string]any)["ram_bytes"] = 1
		}
		body, err := canonical.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		_, err = DecodePerspectiveIntent(body)
		if (err == nil) != (fault == "") {
			t.Fatal(fault, err)
		}
	}
}
func TestPerspectiveServiceScopeRejectsBeforeIO(t *testing.T) {
	tenant, project := uuid.New(), uuid.New()
	service := &PerspectiveService{tenant: tenant, project: project, coordinator: PerspectiveCoordinator{DB: noPerspectivePreparationDB{}, Reader: noPerspectivePreparationIO{}, Writer: noPerspectivePreparationIO{}}}
	intent := PerspectiveIntent{SchemaVersion: 1, Snapshot: uuid.New(), Params: perspectiveIntent()}
	for _, fault := range []string{"tenant", "project", "actor", "session", "key", "intent", "cancel"} {
		who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
		tID, pID, key := tenant, project, "perspective-service-test"
		request := intent
		ctx, cancel := context.WithCancel(context.Background())
		switch fault {
		case "tenant":
			tID = uuid.New()
		case "project":
			pID = uuid.New()
		case "actor":
			who.UserID = uuid.Nil
		case "session":
			who.SessionID = uuid.Nil
		case "key":
			key = ""
		case "intent":
			request.SchemaVersion = 0
		case "cancel":
			cancel()
		}
		_, err := service.SubmitProjectIntent(ctx, who, tID, pID, key, request)
		cancel()
		if err == nil {
			t.Fatal("invalid service request", fault)
		}
	}
}
