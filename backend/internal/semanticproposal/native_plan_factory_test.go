package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeFactoryOwnsOriginalRequestAndRejectsSubstitution(t *testing.T) {
	tenant, actor := uuid.New(), uuid.New()
	who := auth.Identity{UserID: actor, SessionID: uuid.New()}
	pin := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: pin, CanvasHash: strings.Repeat("b", 64)}, ReferenceMask: pin, TargetSnapshotID: uuid.New(), Strength: .5, PreservePersons: true}
	body, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	request, err := CompileRequest(tenant, actor, "native-factory", body)
	if err != nil {
		t.Fatal(err)
	}
	changed := request
	changed.Intent.Strength = .9
	owned, err := ownNativePlanRequest(context.Background(), who, changed, "native-factory")
	if err != nil || owned.Intent.Strength != .5 || owned.RunID != request.RunID || owned.RequestSHA != request.RequestSHA {
		t.Fatal("factory trusted mutable intent", err)
	}
	for _, field := range []string{"run", "workflow", "request_sha", "actor", "session", "key"} {
		changed = request
		identity := who
		key := "native-factory"
		switch field {
		case "run":
			changed.RunID = uuid.New()
		case "workflow":
			changed.WorkflowID = uuid.New()
		case "request_sha":
			changed.RequestSHA = strings.Repeat("f", 64)
		case "actor":
			identity.UserID = uuid.New()
		case "session":
			identity.SessionID = uuid.Nil
		case "key":
			key = "foreign-key"
		}
		if _, err := ownNativePlanRequest(context.Background(), identity, changed, key); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("factory substituted request", field, err)
		}
	}
	owned.Body[0] = 0
	if request.Body[0] != '{' {
		t.Fatal("factory request aliases caller body")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ownNativePlanRequest(ctx, who, request, "native-factory"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
