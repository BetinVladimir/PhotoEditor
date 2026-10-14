package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type assemblyReadDB struct {
	value *string
	args  []any
	calls int
	err   error
}

func (d *assemblyReadDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	d.calls++
	d.args = args
	return assemblyReadRow{d.value, d.err}
}

type assemblyReadRow struct {
	value *string
	err   error
}

func (r assemblyReadRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(**string) = r.value
	return nil
}
func TestReadPublishedAssemblyRechecksExactRootAndFrozenScope(t *testing.T) {
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
	observations := []ChildObservation{{SlotIndex: 0, Export: ExportObservation{ExportID: exportID, SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA, RunState: "completed", ExportState: "ready", ArtifactReady: true, Artifact: &artifact}}}
	_, body, _, err := plan.BuildObservedAssemblyInput(context.Background(), lease.Run, observations, func(_ context.Context, _ string, body []byte) (v2input.Ref, error) {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"ready", "stale", "hash", "policy", "unknown", "whitespace", "parent", "cancel", "sql_error"} {
		t.Run(scenario, func(t *testing.T) {
			raw := string(body)
			ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}
			current := lease
			db := &assemblyReadDB{value: &raw}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "stale":
				db.value = nil
			case "hash":
				raw += " "
			case "whitespace":
				raw += " "
				ref.SHA256 = digest([]byte(raw))
			case "policy", "unknown":
				var value map[string]any
				if json.Unmarshal(body, &value) != nil {
					t.Fatal("fixture")
				}
				if scenario == "policy" {
					value["slot_count"] = 2
				} else {
					value["foreign"] = true
				}
				changed, err := canonical.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				raw = string(changed)
				ref.SHA256 = digest(changed)
			case "parent":
				current.ParentTask = uuid.New()
			case "cancel":
				cancel()
			case "sql_error":
				db.err = errors.New("database unavailable")
			}
			input, exact, err := (ExpansionLeaseStore{DB: db}).ReadPublishedAssembly(ctx, current, plan, ref)
			if scenario == "ready" {
				if err != nil || len(input.Shards) != 1 || string(exact) != raw {
					t.Fatal(input, err)
				}
				if len(db.args) != 4 || db.args[0] != lease.Expansion || db.args[1] != lease.Token || db.args[2] != ref.ArtifactID || db.args[3] != ref.SHA256 {
					t.Fatal("unscoped read", db.args)
				}
			} else {
				if err == nil || exact != nil {
					t.Fatal("invalid root accepted", scenario)
				}
				if (scenario == "parent" || scenario == "cancel") && db.calls != 0 {
					t.Fatal("invalid preflight reached SQL")
				}
			}
		})
	}
}

type assemblyRetainDB struct {
	accepted bool
	err      error
	calls    int
	args     []any
}

func (d *assemblyRetainDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	d.calls++
	d.args = args
	return assemblyRetainRow{d.accepted, d.err}
}

type assemblyRetainRow struct {
	accepted bool
	err      error
}

func (r assemblyRetainRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.accepted
	return nil
}
func TestRetainPublishedAssemblyUsesCurrentLeaseAndServerClosure(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	lease.Cursor = lease.SlotCount
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("assembly"))}
	for _, scenario := range []string{"accepted", "stale", "sql_error", "parent", "cursor", "hash", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			db := &assemblyRetainDB{accepted: scenario != "stale"}
			current := lease
			root := ref
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "sql_error":
				db.err = errors.New("database unavailable")
			case "parent":
				current.ParentTask = uuid.New()
			case "cursor":
				current.Cursor = 0
			case "hash":
				root.SHA256 = "invalid"
			case "cancel":
				cancel()
			}
			err := (ExpansionLeaseStore{DB: db}).RetainPublishedAssembly(ctx, current, plan, root)
			if scenario == "accepted" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid retention accepted", scenario)
			}
			if scenario == "parent" || scenario == "cursor" || scenario == "hash" || scenario == "cancel" {
				if db.calls != 0 {
					t.Fatal("invalid retention reached SQL")
				}
			} else if len(db.args) != 4 || db.args[0] != lease.Expansion || db.args[1] != lease.Token || db.args[2] != ref.ArtifactID || db.args[3] != ref.SHA256 {
				t.Fatal("retention scope drift", db.args)
			}
		})
	}
}
