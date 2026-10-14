package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestRelightRecoveryRejectsCorruptedBinding(t *testing.T) {
	ctx := context.Background()
	prepared, ref := relightFrozenFixture(t, 13)
	frozen, err := freezePreparedRelightTask(ctx, prepared, ref)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := frozen.PublishInputClosure(ctx, &relightTransportRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	binding := map[string]any{"manifest": graph.Built.Manifest, "chunks": graph.Built.Chunks, "count": graph.Built.Count, "sha256": graph.Built.SHA256}
	raw, _ := canonical.Marshal(binding)
	if _, err = restoreRelightRegisteredTransport(ctx, frozen, raw); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"unknown", "count", "sha", "alias", "nil", "chunks"} {
		t.Run(kind, func(t *testing.T) {
			changed := map[string]any{}
			for k, v := range binding {
				changed[k] = v
			}
			switch kind {
			case "unknown":
				changed["extra"] = true
			case "count":
				changed["count"] = 1
			case "sha":
				changed["sha256"] = "bad"
			case "alias":
				r := graph.Built.Manifest
				r.ArtifactID = frozen.entries[0].Ref.ArtifactID
				changed["manifest"] = r
			case "nil":
				r := graph.Built.Manifest
				r.ArtifactID = uuid.Nil
				changed["manifest"] = r
			case "chunks":
				changed["chunks"] = []any{}
			}
			bad, _ := canonical.Marshal(changed)
			if g, e := restoreRelightRegisteredTransport(ctx, frozen, bad); e == nil || g.Open != nil {
				t.Fatal("corrupt recovery returned graph")
			}
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if g, e := restoreRelightRegisteredTransport(canceled, frozen, raw); e == nil || g.Open != nil {
		t.Fatal("canceled recovery")
	}
}
