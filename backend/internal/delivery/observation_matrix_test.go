package delivery

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type matrixObservationStore struct {
	calls, renew          int
	fail, change, pending bool
	observation           ExportObservation
}

func (s *matrixObservationStore) Renew(context.Context, ExpansionLease) error {
	s.renew++
	if s.fail {
		return errors.New("lease lost")
	}
	return nil
}
func (s *matrixObservationStore) ReadObservationPage(context.Context, ExpansionLease, PreparePlan, int, int) ([]ChildObservation, error) {
	s.calls++
	value := s.observation
	if s.change && s.calls > 1 {
		value.ExportState = "queued"
	}
	if s.pending {
		value.RunState = "running"
	}
	return []ChildObservation{{SlotIndex: 0, Export: value}}, nil
}
func TestObservedMatrixRenewalAndStableSecondPass(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	lease.Cursor = lease.SlotCount
	page, err := plan.ExportChildren(lease.Run, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	child := page.Items[0]
	export, err := plan.NewChildExportID(lease.Run, child)
	if err != nil {
		t.Fatal(err)
	}
	artifact := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("raster"))}
	for _, scenario := range []string{"stable", "changed", "pending", "lost"} {
		t.Run(scenario, func(t *testing.T) {
			store := &matrixObservationStore{fail: scenario == "lost", change: scenario == "changed", pending: scenario == "pending", observation: ExportObservation{ExportID: export, SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA, RunState: "completed", ExportState: "ready", ArtifactReady: true, Artifact: &artifact}}
			observations, state, err := plan.ReadObservedMatrix(context.Background(), store, lease)
			switch scenario {
			case "stable":
				if err != nil || state != "assemble" || store.calls != 2 || store.renew != 3 {
					t.Fatal(state, err, store)
				}
				observations[0].Export.Artifact.SHA256 = "changed"
				if artifact.SHA256 == "changed" {
					t.Fatal("borrowed artifact")
				}
			case "pending":
				if err != nil || state != "pending" || store.calls != 1 {
					t.Fatal(state, err, store)
				}
			case "changed":
				if err == nil || len(observations) != 0 {
					t.Fatal("changed matrix accepted")
				}
			case "lost":
				if err == nil || store.calls != 0 {
					t.Fatal("lost lease read")
				}
			}
		})
	}
}
