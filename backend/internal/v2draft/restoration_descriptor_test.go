package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func TestRestorationDescriptorRetainsOriginAndRejectsNoncanonicalReplay(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	}
	entries := make([]v2input.Entry, 6)
	for i := range entries {
		entries[i] = v2input.Entry{Ref: ref(), Contract: "NpyArray"}
	}
	prepared := RestorationResolvedDraft{Draft: v2portrait.RestorationDraft{
		Task: v2input.DraftTask{ID: uuid.New(), Operation: "image.reflection"}, Entries: entries},
		Origin: RestorationManualOrigin{Root: ref(), Params: ref(),
			TargetPixel: ref(), ProtectedPixel: ref()}}
	raw, err := prepared.Descriptor([]byte(`{"mode":"manual"}`))
	if err != nil {
		t.Fatal(err)
	}
	var stored restorationTaskDescriptor
	if json.Unmarshal(raw, &stored) != nil || stored.SchemaVersion != 1 ||
		stored.Origin != prepared.Origin || len(stored.Entries) != 6 ||
		!bytes.Equal(stored.Params, []byte(`{"mode":"manual"}`)) {
		t.Fatal("durable descriptor lost origin lineage")
	}
	if _, err := prepared.Descriptor([]byte(`{ "mode":"manual"}`)); err == nil {
		t.Fatal("noncanonical Params accepted")
	}
	bad := prepared
	bad.Origin.Params = bad.Origin.Root
	if _, err := bad.Descriptor(stored.Params); err == nil {
		t.Fatal("same original root and Params accepted")
	}
	if _, err := RestoreRestorationTaskDescriptor(context.Background(), append(raw, '\n'),
		nil, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), uuid.New(), 0,
		"image.reflection", stored.Params, nil, v2portrait.RestorationQualificationPin{},
		v2input.Ref{}, nil, nil, nil, nil); err == nil {
		t.Fatal("noncanonical persisted descriptor accepted before authority replay")
	}
}
