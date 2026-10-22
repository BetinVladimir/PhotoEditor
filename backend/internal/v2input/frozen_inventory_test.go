package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestFreezeInputInventoryOwnsCallerEntries(t *testing.T) {
	raw := json.RawMessage(`{"schema_version":1}`)
	digest, _ := canonical.Hash(raw)
	params := Ref{ArtifactID: uuid.New(), SHA256: digest}
	entries := []Entry{{Ref: params, Contract: "Params"}, {Ref: Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Contract: "NpyArray"}}
	task := uuid.New()
	closure, err := FreezeInputInventory(task, "semantic.preview_local_exposure", raw, params, entries)
	if err != nil {
		t.Fatal(err)
	}
	entries[0] = Entry{}
	entries[1] = Entry{}
	raw[0] = '!'
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		entry, ok, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if entry.Ref.ArtifactID == uuid.Nil {
			t.Fatal("aliased caller state")
		}
		count++
	}
	if count != 2 {
		t.Fatal("lost inputs")
	}
}
func TestFreezeInputInventoryRejectsMissingParamsAndConflictingIdentity(t *testing.T) {
	raw := json.RawMessage(`{"schema_version":1}`)
	digest, _ := canonical.Hash(raw)
	params := Ref{ArtifactID: uuid.New(), SHA256: digest}
	for _, entries := range [][]Entry{nil, {{Ref: params, Contract: "WorkingImage"}},
		{{Ref: params, Contract: "Params"}, {Ref: Ref{ArtifactID: params.ArtifactID, SHA256: strings.Repeat("0", 64)}, Contract: "Params"}}} {
		if _, err := FreezeInputInventory(uuid.New(), "semantic.preview_local_exposure", raw, params, entries); err == nil {
			t.Fatal("invalid inventory accepted")
		}
	}
}
