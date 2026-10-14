package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestRestoreEnvelopeInventoryCanonicalAndSubstitution(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	tenant := uuid.MustParse("00000000-0000-0000-0000-000000000064")
	event := uuid.MustParse("00000000-0000-0000-0000-0000000000c9")
	inventory := newRestoreEnvelopeInventory()
	if err := inventory.append(id, tenant, event, "tasks", "route", []byte(`{"task":1}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(map[string]any{"id": id.String(), "tenant_id": tenant.String(), "event_id": event.String(), "destination": "tasks", "routing_key": "route", "payload": map[string]any{"task": 1}})
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(append(append([]byte("photoeditor.restore-task-outbox/v1\n"), raw...), '\n'))
	if inventory.count != 1 || hex.EncodeToString(inventory.digest.Sum(nil)) != hex.EncodeToString(expected[:]) {
		t.Fatal("canonical inventory mismatch")
	}
	changed := newRestoreEnvelopeInventory()
	if err = changed.append(id, tenant, event, "tasks", "route", []byte(`{"task":999}`)); err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(changed.digest.Sum(nil)) == hex.EncodeToString(inventory.digest.Sum(nil)) {
		t.Fatal("payload substitution invisible")
	}
	if err = inventory.append(id, tenant, event, "tasks", "route", []byte(`{}`)); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestRestoreEnvelopeInventoryRefusals(t *testing.T) {
	for _, mode := range []string{"nil_id", "nil_tenant", "nil_event", "empty_destination", "array_payload", "null_payload", "invalid_payload", "reversed"} {
		t.Run(mode, func(t *testing.T) {
			id, tenant, event := uuid.New(), uuid.New(), uuid.New()
			destination := "tasks"
			payload := []byte(`{}`)
			inventory := newRestoreEnvelopeInventory()
			switch mode {
			case "nil_id":
				id = uuid.Nil
			case "nil_tenant":
				tenant = uuid.Nil
			case "nil_event":
				event = uuid.Nil
			case "empty_destination":
				destination = ""
			case "array_payload":
				payload = []byte(`[]`)
			case "null_payload":
				payload = []byte(`null`)
			case "invalid_payload":
				payload = []byte(`{`)
			case "reversed":
				inventory.previous = "ffffffff-ffff-ffff-ffff-ffffffffffff"
			}
			if err := inventory.append(id, tenant, event, destination, "route", payload); err == nil {
				t.Fatal("invalid envelope accepted")
			}
			if inventory.count != 0 {
				t.Fatal("failed row changed inventory")
			}
		})
	}
}

func TestRestoreTaskEnvelopeMatchesPublisherScope(t *testing.T) {
	for _, destination := range []string{Commands, Events} {
		if !restoreTaskEnvelopePublishable(destination, []byte(`{}`)) {
			t.Fatalf("claimable task destination refused: %q", destination)
		}
	}
	for _, destination := range []string{"", "tasks", "photo.mail.v1", "photo.commands.v2", "photo.events.v1.extra"} {
		if restoreTaskEnvelopePublishable(destination, []byte(`{}`)) {
			t.Fatalf("unclaimable task destination accepted: %q", destination)
		}
	}
	if !restoreTaskEnvelopePublishable(Commands, make([]byte, 65536)) || restoreTaskEnvelopePublishable(Events, make([]byte, 65537)) {
		t.Fatal("publisher payload size boundary changed")
	}
}

func TestRestoreInventoryMissingDatabaseRefusesBeforeGuard(t *testing.T) {
	calls := 0
	guard := func(context.Context) error { calls++; return nil }
	var missing *Outbox
	for _, outbox := range []*Outbox{missing, {}} {
		if err := outbox.verifyRestoreInventory(context.Background(), "isolated", 0, strings.Repeat("a", 64), guard); err == nil {
			t.Fatal("missing database accepted")
		}
	}
	if calls != 0 {
		t.Fatal("guard invoked before structural validation")
	}
}

func TestSharedPythonGoRestoreInventoryGoldenBytes(t *testing.T) {
	raw, err := os.ReadFile("../../../tests/fixtures/restore-task-outbox-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Schema string            `json:"schema_version"`
		Domain string            `json:"domain"`
		Rows   []json.RawMessage `json:"rows"`
		Lines  []string          `json:"canonical_lines"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Schema != "restore-task-outbox-golden/v1" || fixture.Domain != "photoeditor.restore-task-outbox/v1\n" || len(fixture.Rows) != len(fixture.Lines) {
		t.Fatal("invalid fixture")
	}
	inventory := newRestoreEnvelopeInventory()
	for n, rawRow := range fixture.Rows {
		canonicalRow, err := canonical.Bytes(rawRow)
		if err != nil {
			t.Fatal(err)
		}
		if string(canonicalRow)+"\n" != fixture.Lines[n] {
			t.Fatal("canonical golden bytes mismatch", n)
		}
		var row struct {
			ID          uuid.UUID       `json:"id"`
			Tenant      uuid.UUID       `json:"tenant_id"`
			Event       uuid.UUID       `json:"event_id"`
			Destination string          `json:"destination"`
			Routing     string          `json:"routing_key"`
			Payload     json.RawMessage `json:"payload"`
		}
		if err = json.Unmarshal(rawRow, &row); err != nil {
			t.Fatal(err)
		}
		if err = inventory.append(row.ID, row.Tenant, row.Event, row.Destination, row.Routing, row.Payload); err != nil {
			t.Fatal(err)
		}
	}
	expected := sha256.Sum256([]byte(fixture.Domain + strings.Join(fixture.Lines, "")))
	if hex.EncodeToString(inventory.digest.Sum(nil)) != hex.EncodeToString(expected[:]) {
		t.Fatal("cross-language inventory golden mismatch")
	}
}
