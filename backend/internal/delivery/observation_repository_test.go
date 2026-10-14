package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestExportObservationRepositoryBindsFrozenIdentity(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	page, err := plan.ExportChildren(lease.Run, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	child := page.Items[0]
	lease.Cursor = 1
	export, err := plan.NewChildExportID(lease.Run, child)
	if err != nil {
		t.Fatal(err)
	}
	original := ExportObservation{ExportID: export, SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA, RunState: "running", ExportState: "queued"}
	for _, scenario := range []string{"valid", "stale", "export", "binding", "digest", "extra"} {
		t.Run(scenario, func(t *testing.T) {
			observed := original
			switch scenario {
			case "export":
				observed.ExportID = uuid.New()
			case "binding":
				observed.Binding.AssetVersionID = uuid.New()
			case "digest":
				observed.SnapshotDigest = "foreign"
			}
			raw, _ := json.Marshal(observed)
			if scenario == "stale" {
				raw = []byte("null")
			}
			if scenario == "extra" {
				raw = append(raw[:len(raw)-1], []byte(",\"extra\":true}")...)
			}
			store := ExpansionLeaseStore{DB: &deliveryReadTx{raw: raw}}
			result, err := store.ReadExportObservation(context.Background(), lease, plan, child)
			if scenario == "valid" {
				if err != nil || result.ExportID != export {
					t.Fatal(result, err)
				}
			} else if err == nil {
				t.Fatal("foreign observation accepted")
			}
		})
	}
}

func TestObservationPageRejectsMissingOrSubstitutedSlots(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	lease.Cursor = lease.SlotCount
	children, err := plan.ExportChildren(lease.Run, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	child := children.Items[0]
	export, err := plan.NewChildExportID(lease.Run, child)
	if err != nil {
		t.Fatal(err)
	}
	original := ChildObservation{SlotIndex: 0, Export: ExportObservation{ExportID: export, Binding: child.Binding, SnapshotDigest: child.SnapshotDigest, PresetSHA: child.Preset.PresetSHA, RunState: "running", ExportState: "queued"}}
	for _, scenario := range []string{"valid", "missing", "duplicate", "slot", "foreign", "null"} {
		t.Run(scenario, func(t *testing.T) {
			value := original
			if scenario == "slot" {
				value.SlotIndex = 1
			}
			if scenario == "foreign" {
				value.Export.ExportID = uuid.New()
			}
			values := []ChildObservation{value}
			if scenario == "missing" {
				values = []ChildObservation{}
			}
			if scenario == "duplicate" {
				values = append(values, value)
			}
			raw, _ := json.Marshal(values)
			if scenario == "null" {
				raw = []byte("null")
			}
			db := &deliveryReadTx{raw: raw}
			result, err := (ExpansionLeaseStore{DB: db}).ReadObservationPage(context.Background(), lease, plan, 0, 100)
			if scenario == "valid" {
				if err != nil || len(result) != 1 {
					t.Fatal(result, err)
				}
			} else if err == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
}
