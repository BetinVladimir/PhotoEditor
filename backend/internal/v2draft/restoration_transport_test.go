package v2draft

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func TestRestorationTransportPublishesFrozenInventoryAndRejectsSubstitution(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	entries := make([]v2input.Entry, 6)
	for i := range entries {
		entries[i] = v2input.Entry{Ref: ref(), Contract: "NpyArray"}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	prepared := RestorationResolvedDraft{Draft: v2portrait.RestorationDraft{
		Task: v2input.DraftTask{ID: uuid.New(), Operation: "image.reflection"}, Entries: entries},
		Origin: RestorationManualOrigin{Root: ref(), Params: ref(), TargetPixel: ref(), ProtectedPixel: ref()}}
	params := []byte(`{"schema_version":1}`)
	graph, err := prepared.PublishInputClosure(context.Background(), params, donorClosureTestPublisher{})
	if err != nil || graph.ID != prepared.Draft.Task.ID || graph.Built.Count != len(entries) ||
		len(graph.Built.Chunks) != 1 || len(graph.Direct) != len(entries) {
		t.Fatal("frozen closure publication failed", err)
	}
	cursor, err := graph.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range entries {
		got, ok, e := cursor.Next(context.Background())
		if e != nil || !ok || got != want {
			t.Fatal("reopened cursor changed", e)
		}
	}
	if _, ok, e := cursor.Next(context.Background()); e != nil || ok {
		t.Fatal("cursor overran", e)
	}
	wire, err := canonical.Marshal(restorationRegisteredTransportWire{
		Manifest: graph.Built.Manifest, Chunks: graph.Built.Chunks,
		Count: graph.Built.Count, SHA: graph.Built.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restoreRestorationRegisteredTransport(context.Background(), prepared, wire)
	if err != nil || recovered.ID != graph.ID || recovered.Built.SHA256 != graph.Built.SHA256 ||
		len(recovered.Direct) != len(graph.Direct) {
		t.Fatal("registered transport not recovered", err)
	}
	changedWire := append([]byte(nil), wire...)
	changedWire = append(changedWire, '\n')
	if _, err := restoreRestorationRegisteredTransport(context.Background(), prepared, changedWire); err == nil {
		t.Fatal("noncanonical retained transport accepted")
	}
	changed := graph
	changed.Direct = append([]v2input.Ref(nil), graph.Direct...)
	changed.Direct[0] = ref()
	tx := &restorationStoreTx{role: "photo_scheduler", accepted: true}
	store := SQLRestorationDescriptorStore{DB: restorationStoreDB{tx}}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if err := store.RegisterTransport(context.Background(), who, uuid.New(), uuid.New(),
		prepared, params, changed); err == nil || len(tx.queries) != 0 {
		t.Fatal("substituted direct input reached SQL")
	}
	tx.read = wire
	retained, err := store.ReadRegisteredTransport(context.Background(), who, uuid.New(), uuid.New(),
		prepared, params)
	if err != nil || retained.ID != graph.ID || tx.commits != 1 ||
		!strings.Contains(tx.queries[len(tx.queries)-1], "read_v28_restoration_registered_transport") {
		t.Fatal("SQL retained transport path changed", err)
	}
}
