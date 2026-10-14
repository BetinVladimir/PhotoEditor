package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestCreateDocumentsCanonicalPolicyWithReversePresetOrder(t *testing.T) {
	base, session, catalog, items := createDocumentsFixture(t)
	high := uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	low := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	request := base.Request()
	request.Policy.RequiredPresets = []uuid.UUID{high, low}
	intent, err := FreezeCreateIntent(base.tenant, auth.Identity{UserID: base.actor, SessionID: session}, request, "multi-preset")
	if err != nil {
		t.Fatal(err)
	}
	first := catalog.Presets[0]
	first.ID = high
	second := first
	second.ID = low
	catalog.Presets = []CreateCatalogPreset{first, second}
	docs, err := intent.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	var frozen FrozenInput
	if err := json.Unmarshal(docs.Frozen.Body, &frozen); err != nil {
		t.Fatal(err)
	}
	expected, err := canonical.Marshal(frozen.Policy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, docs.Policy.Body) || docs.Policy.Ref.SHA256 != digest(expected) {
		t.Fatal("policy does not bind frozen canonical order")
	}
	if got := intent.Request().Policy.RequiredPresets; got[0] != high || got[1] != low {
		t.Fatal("request ordering mutated")
	}
	if docs.Presets[0].Ref.ArtifactID == docs.Presets[1].Ref.ArtifactID {
		t.Fatal("same config merged distinct presets")
	}
	raw, err := intent.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intent.DecodePending(context.Background(), uuid.New(), raw); err != nil {
		t.Fatalf("session-rotation recovery: %v", err)
	}
}

func createDocumentsFixture(t *testing.T) (CreateIntent, uuid.UUID, CreateCatalog, []v2input.SessionSnapshotItem) {
	t.Helper()
	_, snapshotRaw, policy, configs := freezeFixture(t)
	var snapshot v2input.SessionInputSnapshot
	if err := json.Unmarshal(snapshotRaw, &snapshot); err != nil {
		t.Fatal(err)
	}
	session := uuid.New()
	intent, err := FreezeCreateIntent(uuid.New(), auth.Identity{UserID: uuid.New(), SessionID: session}, CreateRequest{ProjectID: uuid.New(), SnapshotID: snapshot.SnapshotID, Name: "Delivery", Policy: policy}, "key")
	if err != nil {
		t.Fatal(err)
	}
	catalog := CreateCatalog{Snapshot: snapshot.SnapshotID, SnapshotDigest: snapshot.SnapshotDigest, ItemCount: len(snapshot.Items), Presets: []CreateCatalogPreset{{ID: configs[0].ID, Revision: "1", SHA256: configs[0].SHA, ConfigJCS: configs[0].Raw}}}
	return intent, session, catalog, snapshot.Items
}
func TestCreateDocumentsDeterministicOwnedAndPlanBound(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	first, err := intent.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	second, err := intent.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	for index, doc := range []CreateDocument{first.Snapshot, first.Policy, first.Frozen, first.Params, first.Presets[0]} {
		other := []CreateDocument{second.Snapshot, second.Policy, second.Frozen, second.Params, second.Presets[0]}[index]
		if doc.Ref != other.Ref || !bytes.Equal(doc.Body, other.Body) || digest(doc.Body) != doc.Ref.SHA256 {
			t.Fatal("unstable document", doc.Purpose)
		}
	}
	if first.Plan.TaskID() != intent.Identity().PrepareTask || !bytes.Equal(first.Params.Body, first.Plan.ParamsJCS()) {
		t.Fatal("plan identity mismatch")
	}
	catalog.Presets[0].ConfigJCS[0] = 'x'
	items[0].Recipe.ArtifactID = uuid.New()
	if !json.Valid(first.Presets[0].Body) || first.Snapshot.Ref != second.Snapshot.Ref {
		t.Fatal("borrowed input bytes")
	}
	first.Params.Body[0] = 'x'
	if first.Plan.ParamsJCS()[0] != '{' {
		t.Fatal("plan borrows public body")
	}
}
func TestCreateDocumentsRefusesWrongInventoryAndConfigs(t *testing.T) {
	for _, mutate := range []func(*CreateCatalog, []v2input.SessionSnapshotItem){
		func(c *CreateCatalog, _ []v2input.SessionSnapshotItem) { c.Snapshot = uuid.New() },
		func(c *CreateCatalog, _ []v2input.SessionSnapshotItem) { c.ItemCount++ },
		func(c *CreateCatalog, _ []v2input.SessionSnapshotItem) { c.Presets = nil },
		func(c *CreateCatalog, _ []v2input.SessionSnapshotItem) { c.Presets[0].ID = uuid.New() },
		func(c *CreateCatalog, _ []v2input.SessionSnapshotItem) { c.Presets[0].ConfigJCS = []byte(`{}`) },
		func(_ *CreateCatalog, items []v2input.SessionSnapshotItem) { items[0].Recipe.ArtifactID = uuid.Nil },
	} {
		intent, session, catalog, items := createDocumentsFixture(t)
		mutate(&catalog, items)
		if _, err := intent.CompileDocuments(context.Background(), session, catalog, items); err == nil {
			t.Fatal("corrupt source accepted")
		}
	}
	intent, session, catalog, items := createDocumentsFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := intent.CompileDocuments(ctx, session, catalog, items); err == nil {
		t.Fatal("cancel ignored")
	}
}
