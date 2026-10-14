package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestAssemblyRequiredInputsComeFromExactFrozenMatrix(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
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
	observations := []ChildObservation{{SlotIndex: 0, Export: ExportObservation{ExportID: exportID, SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA, RunState: "completed", ExportState: "ready", ArtifactReady: true, Artifact: &artifact}}}
	var shards [][]byte
	input, body, sha, err := plan.BuildObservedAssemblyInput(context.Background(), lease.Run, observations, func(_ context.Context, _ string, raw []byte) (v2input.Ref, error) {
		shards = append(shards, append([]byte(nil), raw...))
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(raw)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	for _, scenario := range []string{"ready", "missing_shard", "corrupt_shard", "lost_lease", "stale_final"} {
		t.Run("recovery_"+scenario, func(t *testing.T) {
			current := lease
			current.Cursor = current.SlotCount
			db := &assemblyInputsDB{root: string(body), shard: string(shards[0]), scenario: scenario}
			inputs, err := (ExpansionLeaseStore{DB: db}).ReadPublishedAssemblyInputs(context.Background(), current, plan, root)
			if scenario == "ready" {
				if err != nil || len(inputs) < 6 || db.roots != 2 || db.shards != 1 || db.renews != 2 {
					t.Fatal("incomplete recovered matrix", err, db)
				}
			} else if err == nil || inputs != nil {
				t.Fatal("invalid recovery yielded inputs", scenario)
			}
		})
	}

	required, err := plan.AssemblyRequiredInputs(context.Background(), lease.Run, root, body, shards)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid_inventory", "missing_input", "duplicate_input", "wrong_contract", "unknown_field", "null_inventory"} {
		t.Run(scenario, func(t *testing.T) {
			current := lease
			current.Cursor = current.SlotCount
			values := append([]v2input.Entry(nil), required...)
			switch scenario {
			case "missing_input":
				values = values[1:]
			case "duplicate_input":
				values = append(values, values[0])
			case "wrong_contract":
				values[0].Contract = "foreign"
			}
			payload, err := json.Marshal(values)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "unknown_field" {
				payload = []byte(`[{"unexpected":true}]`)
			}
			if scenario == "null_inventory" {
				payload = []byte("null")
			}
			db := &assemblyInputsDB{root: string(body), shard: string(shards[0]), inventory: payload}
			entries, err := (ExpansionLeaseStore{DB: db}).ResolvePublishedAssemblyInputs(context.Background(), current, plan, root)
			if scenario == "valid_inventory" {
				if err != nil || len(entries) != len(required) {
					t.Fatal("valid inventory refused", err)
				}
			} else if err == nil || entries != nil {
				t.Fatal("invalid inventory accepted", scenario)
			}
		})
	}
	inventory := map[uuid.UUID]v2input.Entry{}
	for _, entry := range required {
		inventory[entry.Ref.ArtifactID] = entry
	}
	for _, entry := range []v2input.Entry{{Ref: root, Contract: "DeliveryAssemblyInput"}, {Ref: input.Shards[0], Contract: "DeliverySlotShard"}, {Ref: child.Preset.Config, Contract: "ExportPresetConfig"}, {Ref: artifact, Contract: "EncodedExport"}, {Ref: child.Binding.Source, Contract: "WorkingImage"}, {Ref: child.Recipe, Contract: "FrozenRenderRecipe"}} {
		if inventory[entry.Ref.ArtifactID] != entry {
			t.Fatal("missing independently required input", entry)
		}
	}
	for _, scenario := range []string{"root_hash", "shard_hash", "missing", "parent", "root_alias", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			current := root
			raw := append([]byte(nil), body...)
			parts := append([][]byte(nil), shards...)
			parent := lease.Run
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "root_hash":
				raw = append(raw, ' ')
			case "shard_hash":
				parts[0] = append(append([]byte(nil), parts[0]...), ' ')
			case "missing":
				parts = nil
			case "parent":
				parent = uuid.New()
			case "root_alias":
				current.ArtifactID = artifact.ArtifactID
			case "cancel":
				cancel()
			}
			if inputs, err := plan.AssemblyRequiredInputs(ctx, parent, current, raw, parts); err == nil || inputs != nil {
				t.Fatal("invalid matrix yielded inputs", scenario)
			}
		})
	}
}

type assemblyInputsDB struct {
	root, shard, scenario string
	inventory             []byte
	roots, shards, renews int
}

func (d *assemblyInputsDB) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	switch {
	case strings.Contains(q, "v30_delivery_assembly_inventory"):
		return assemblyInventoryRow{raw: d.inventory}
	case strings.Contains(q, "update_v30_delivery_expansion_lease"):
		d.renews++
		return assemblyRetainRow{accepted: d.scenario != "lost_lease"}
	case strings.Contains(q, "v30_delivery_saved_shard"):
		d.shards++
		if d.scenario == "missing_shard" {
			return assemblyReadRow{}
		}
		value := d.shard
		if d.scenario == "corrupt_shard" {
			value += " "
		}
		return assemblyReadRow{value: &value}
	case strings.Contains(q, "v30_delivery_saved_assembly"):
		d.roots++
		if d.scenario == "stale_final" && d.roots > 1 {
			return assemblyReadRow{}
		}
		value := d.root
		return assemblyReadRow{value: &value}
	default:
		panic("unexpected recovery SQL")
	}
}

type assemblyInventoryRow struct{ raw []byte }

func (r assemblyInventoryRow) Scan(dest ...any) error { *dest[0].(*[]byte) = r.raw; return nil }
