package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestCreateSelectionBindsPinnedRecipeAndIdentity(t *testing.T) {
	intent, who, tid, _ := createCatalogFixture(t)
	_, snapshotRaw, _, _ := freezeFixture(t)
	var snapshot v2input.SessionInputSnapshot
	if err := json.Unmarshal(snapshotRaw, &snapshot); err != nil {
		t.Fatal(err)
	}
	recipes := []v2input.Ref{snapshot.Items[0].Recipe}
	raw, _ := json.Marshal(map[string]any{"snapshot_id": intent.Request().SnapshotID, "items": snapshot.Items})
	tx := &deliveryReadTx{raw: raw}
	items, err := intent.ResolveSelection(context.Background(), tx, who, tid, recipes)
	if err != nil || len(items) != 1 || items[0].Recipe != recipes[0] {
		t.Fatal(items, err)
	}
	if len(tx.args) != 6 || tx.args[0] != tid || tx.args[1] != intent.Request().ProjectID || tx.args[2] != intent.Request().SnapshotID || tx.args[3] != who.UserID || tx.args[4] != who.SessionID {
		t.Fatal("scope changed", tx.args)
	}
	var transmitted []v2input.Ref
	if json.Unmarshal([]byte(tx.args[5].(string)), &transmitted) != nil || len(transmitted) != 1 || transmitted[0] != recipes[0] {
		t.Fatal("recipe changed")
	}
	recipes[0].ArtifactID = uuid.New()
	if items[0].Recipe == recipes[0] {
		t.Fatal("borrowed recipe")
	}
	tx.calls = 0
	if _, err := intent.ResolveSelection(context.Background(), tx, who, uuid.New(), transmitted); err == nil || tx.calls != 0 {
		t.Fatal("foreign tenant queried")
	}
}
func TestCreateSelectionRejectsInvalidStaleAndSubstitutedRecipe(t *testing.T) {
	intent, who, tid, _ := createCatalogFixture(t)
	_, snapshotRaw, _, _ := freezeFixture(t)
	var snapshot v2input.SessionInputSnapshot
	_ = json.Unmarshal(snapshotRaw, &snapshot)
	recipes := []v2input.Ref{snapshot.Items[0].Recipe}
	raw, _ := json.Marshal(map[string]any{"snapshot_id": intent.Request().SnapshotID, "items": snapshot.Items})
	tx := &deliveryReadTx{raw: raw}
	for _, refs := range [][]v2input.Ref{nil, {{ArtifactID: uuid.Nil, SHA256: recipes[0].SHA256}}, {{ArtifactID: uuid.New(), SHA256: "broken"}}} {
		if _, err := intent.ResolveSelection(context.Background(), tx, who, tid, refs); err == nil || tx.calls != 0 {
			t.Fatal("invalid recipe queried")
		}
	}
	changed := []v2input.Ref{{ArtifactID: uuid.New(), SHA256: recipes[0].SHA256}}
	if _, err := intent.ResolveSelection(context.Background(), tx, who, tid, changed); err == nil {
		t.Fatal("substituted recipe accepted")
	}
	tx.raw = []byte("null")
	if _, err := intent.ResolveSelection(context.Background(), tx, who, tid, recipes); err == nil {
		t.Fatal("stale accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx.calls = 0
	if _, err := intent.ResolveSelection(ctx, tx, who, tid, recipes); err == nil || tx.calls != 0 {
		t.Fatal("cancelled IO")
	}
}
