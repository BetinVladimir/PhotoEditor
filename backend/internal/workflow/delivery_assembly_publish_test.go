package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

type deliveryAssemblyDB struct{ args [][]any }

func (d *deliveryAssemblyDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.args = append(d.args, append([]any(nil), args...))
	if strings.Contains(query, "retain_v30_delivery_assembly") {
		return deliveryParamsRow{accepted: true}
	}
	return deliveryParamsRow{outcome: "replayed", id: uuid.New(), token: uuid.New()}
}

type deliveryAssemblyNoIO struct{}

func (deliveryAssemblyNoIO) PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error) {
	return objectstore.Object{}, errors.New("unexpected PUT")
}
func (deliveryAssemblyNoIO) RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error) {
	return objectstore.Object{}, errors.New("unexpected recovery")
}

type deliveryAssemblyMatrix struct {
	observation delivery.ExportObservation
	renew       int
	failAt      int
}

func (s *deliveryAssemblyMatrix) Renew(context.Context, delivery.ExpansionLease) error {
	s.renew++
	if s.renew == s.failAt {
		return errors.New("lost authority")
	}
	return nil
}
func (s *deliveryAssemblyMatrix) ReadObservationPage(context.Context, delivery.ExpansionLease, delivery.PreparePlan, int, int) ([]delivery.ChildObservation, error) {
	return []delivery.ChildObservation{{SlotIndex: 0, Export: s.observation}}, nil
}

func TestPublishDeliveryAssemblyInputCarriesExactPublishedShardBytes(t *testing.T) {
	plan, parent, child, _, _ := deliveryExportPlanFixture(t, nil)
	workflow, _ := v2input.WorkflowID(parent)
	exportID, err := plan.NewChildExportID(parent, child)
	if err != nil {
		t.Fatal(err)
	}
	lease := delivery.ExpansionLease{Package: uuid.New(), Expansion: uuid.New(), Token: uuid.New(), Tenant: uuid.New(), Run: parent, Workflow: workflow, ParentTask: plan.TaskID(), Until: time.Now().Add(time.Minute), SlotCount: 1, Cursor: 1}
	artifact := v2input.Ref{ArtifactID: uuid.New(), SHA256: downloadDigest("raster")}
	for _, scenario := range []string{"ready", "pending", "lose_before_root", "lose_after_root"} {
		t.Run(scenario, func(t *testing.T) {
			db := &deliveryAssemblyDB{}
			store := &deliveryAssemblyMatrix{observation: delivery.ExportObservation{ExportID: exportID, SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA, RunState: "completed", ExportState: "ready", ArtifactReady: true, Artifact: &artifact}}
			if scenario == "pending" {
				store.observation.RunState = "running"
			}
			if scenario == "lose_before_root" {
				store.failAt = 6
			}
			if scenario == "lose_after_root" {
				store.failAt = 7
			}
			ref, state, err := PublishDeliveryAssemblyInput(context.Background(), db, deliveryAssemblyNoIO{}, "documents", store, lease, plan)
			switch scenario {
			case "ready":
				if err != nil || state != "assemble" || ref.ArtifactID == uuid.Nil || len(db.args) != 3 {
					t.Fatal(state, ref, err, len(db.args))
				}
				var proof []string
				if json.Unmarshal([]byte(db.args[1][3].(string)), &proof) != nil || len(proof) != 1 || proof[0] != db.args[0][3].(string) {
					t.Fatal("root did not carry exact published shard")
				}
				if renderDerivativeBytesSHA([]byte(db.args[1][2].(string))) != ref.SHA256 {
					t.Fatal("wrong root ref")
				}
			case "pending":
				if err != nil || state != "pending" || ref.ArtifactID != uuid.Nil || len(db.args) != 0 {
					t.Fatal(state, err, len(db.args))
				}
			default:
				if err == nil || ref.ArtifactID != uuid.Nil || state != "" {
					t.Fatal("lost authority returned root", ref, state, err)
				}
				expected := 1
				if scenario == "lose_after_root" {
					expected = 2
				}
				if len(db.args) != expected {
					t.Fatal("wrong publication boundary", len(db.args))
				}
			}
		})
	}
}
