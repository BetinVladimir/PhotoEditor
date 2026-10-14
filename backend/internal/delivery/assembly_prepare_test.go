package delivery

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPrepareObservedAssemblyPublicationBoundaries(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	lease.Cursor = lease.SlotCount
	page, err := plan.ExportChildren(lease.Run, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	child := page.Items[0]
	exportID, err := plan.NewChildExportID(lease.Run, child)
	if err != nil {
		t.Fatal(err)
	}
	artifact := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("raster"))}
	for _, scenario := range []string{"ready", "pending", "changed", "lost", "publish_error", "lost_after_publish", "cancel_after_publish"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &matrixObservationStore{pending: scenario == "pending", change: scenario == "changed", fail: scenario == "lost", observation: ExportObservation{ExportID: exportID, SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA, RunState: "completed", ExportState: "ready", ArtifactReady: true, Artifact: &artifact}}
			calls := 0
			input, body, hash, state, err := plan.PrepareObservedAssembly(ctx, store, lease, func(_ context.Context, purpose string, body []byte) (v2input.Ref, error) {
				calls++
				if purpose != "v30_delivery_slots_000" {
					t.Fatal(purpose)
				}
				if scenario == "publish_error" {
					return v2input.Ref{}, errors.New("object unavailable")
				}
				if scenario == "lost_after_publish" {
					store.fail = true
				}
				if scenario == "cancel_after_publish" {
					cancel()
				}
				return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}, nil
			})
			switch scenario {
			case "ready":
				if err != nil || state != "assemble" || calls != 1 || len(input.Shards) != 1 || digest(body) != hash || store.renew != 5 {
					t.Fatal(state, calls, store.renew, err)
				}
			case "pending":
				if err != nil || state != "pending" || calls != 0 || body != nil {
					t.Fatal(state, calls, err)
				}
			default:
				if err == nil || body != nil || hash != "" || state != "" {
					t.Fatal("failure exposed assembly", state, err)
				}
				if (scenario == "changed" || scenario == "lost") && calls != 0 {
					t.Fatal("published before complete observation")
				}
			}
		})
	}
}
