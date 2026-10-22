package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

func TestAssemblyResourcesUseVerifiedDocumentsAndFullInventory(t *testing.T) {
	plan, parent, child, _, _ := deliveryExportPlanFixture(t, nil)
	exportID, err := plan.NewChildExportID(parent, child)
	if err != nil {
		t.Fatal(err)
	}
	artifact := v2input.Ref{ArtifactID: uuid.New(), SHA256: downloadDigest("raster")}
	observations := []delivery.ChildObservation{{SlotIndex: 0, Export: delivery.ExportObservation{ExportID: exportID, SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA, RunState: "completed", ExportState: "ready", ArtifactReady: true, Artifact: &artifact}}}
	var shards [][]byte
	_, raw, sha, err := plan.BuildObservedAssemblyInput(context.Background(), parent, observations, func(_ context.Context, _ string, body []byte) (v2input.Ref, error) {
		shards = append(shards, append([]byte(nil), body...))
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(body)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	required, err := plan.AssemblyRequiredInputs(context.Background(), parent, root, raw, shards)
	if err != nil {
		t.Fatal(err)
	}
	limits := DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 1 << 30, OutputBytes: 8 << 20}
	workflow, _ := v2input.WorkflowID(parent)
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Tenant: uuid.New(), Run: parent, Workflow: workflow, ParentTask: plan.TaskID(), Until: time.Now().Add(time.Minute), SlotCount: 1, Cursor: 1}
	paramsSHA, err := canonical.Hash(v2input.DeliveryAssembleParams{SchemaVersion: 1, Assembly: root})
	if err != nil {
		t.Fatal(err)
	}
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: paramsSHA}
	for _, scenario := range []string{"ready", "read_error", "lost_lease", "bad_document", "cancel"} {
		t.Run("current_"+scenario, func(t *testing.T) {
			store := &assemblyPreparationStore{evidence: delivery.AssemblyPreparationEvidence{RootJCS: raw, ShardsJCS: shards, Inventory: required}, failRead: scenario == "read_error", failRenew: scenario == "lost_lease"}
			if scenario == "bad_document" {
				store.evidence.RootJCS = []byte(`{}`)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancel" {
				cancel()
			}
			frozen, err := PrepareCurrentDeliveryAssemblyTask(ctx, store, lease, plan, root, params, limits)
			if scenario == "ready" {
				draft, draftErr := frozen.DraftTask()
				if err != nil || draftErr != nil || draft.ParamsSHA != paramsSHA || store.reads != 1 || store.renews != 1 {
					t.Fatal("current task not prepared", err, draftErr)
				}
			} else {
				if err == nil {
					t.Fatal("invalid current evidence accepted", scenario)
				}
				if _, draftErr := frozen.DraftTask(); draftErr == nil {
					t.Fatal("failure returned usable task")
				}
			}
			if scenario == "cancel" && store.reads != 0 {
				t.Fatal("cancelled preparation read SQL")
			}
		})
	}
	for _, scenario := range []string{"ready", "budget", "missing_inventory", "lost_lease", "cancel_after_publish"} {
		t.Run("prepublication_"+scenario, func(t *testing.T) {
			store := &assemblyPreparationStore{evidence: delivery.AssemblyPreparationEvidence{RootJCS: raw, ShardsJCS: shards, Inventory: required}, failRenew: scenario == "lost_lease"}
			caps := limits
			if scenario == "budget" { caps.RAMBytes = 1 }
			if scenario == "missing_inventory" { store.evidence.Inventory = nil }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			published := 0
			frozen, err := prepareDeliveryAssemblyBeforePublication(ctx, store, lease, plan, root, caps, func() (v2input.Ref, error) {
				published++
				if scenario == "cancel_after_publish" { cancel() }
				return params, nil
			})
			if scenario == "ready" {
				if _, draftErr := frozen.DraftTask(); err != nil || draftErr != nil || published != 1 { t.Fatal(err, draftErr, published) }
			} else {
				if err == nil { t.Fatal("invalid preparation accepted") }
				if _, draftErr := frozen.DraftTask(); draftErr == nil { t.Fatal("failure returned usable task") }
				if scenario != "cancel_after_publish" && published != 0 { t.Fatal("invalid preparation wrote Params") }
				if scenario == "cancel_after_publish" && !errors.Is(err, context.Canceled) { t.Fatal(err) }
			}
		})
	}
	profile, err := CompileDeliveryAssemblyProfile(context.Background(), plan, parent, root, raw, shards, required, limits)
	if err != nil {
		t.Fatal(err)
	}
	estimate, err := ParseResourceEstimate(profile)
	if err != nil || estimate.VRAMBytes != 0 || estimate.PixelCount != 0 || estimate.OutputBytes != 8<<20 {
		t.Fatal(estimate, err)
	}
	documents := int64(len(raw) + len(shards[0]))
	if estimate.RAMBytes != (128<<20)+documents*16+int64(len(required)+1)*1024 || estimate.ScratchBytes != documents+(8<<20) {
		t.Fatal("resource accounting drift", estimate)
	}
	extra := append(append([]v2input.Entry(nil), required...), v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: downloadDigest("pixel")}, Contract: "NpyArray"})
	larger, err := CompileDeliveryAssemblyProfile(context.Background(), plan, parent, root, raw, shards, extra, limits)
	if err != nil {
		t.Fatal(err)
	}
	extraEstimate, err := ParseResourceEstimate(larger)
	if err != nil || extraEstimate.RAMBytes != estimate.RAMBytes+1024 {
		t.Fatal("transitive inventory not counted", err)
	}
	for _, scenario := range []string{"ram", "scratch", "output", "corrupt", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			cap := limits
			body := raw
			inventory := required
			switch scenario {
			case "ram":
				cap.RAMBytes = estimate.RAMBytes - 1
			case "scratch":
				cap.ScratchBytes = estimate.ScratchBytes - 1
			case "output":
				cap.OutputBytes = estimate.OutputBytes - 1
			case "corrupt":
				body = append(append([]byte(nil), raw...), ' ')
			case "missing":
				inventory = nil
			}
			if _, err := CompileDeliveryAssemblyProfile(context.Background(), plan, parent, root, body, shards, inventory, cap); err == nil {
				t.Fatal("invalid resource preparation accepted", scenario)
			}
		})
	}
}

type assemblyPreparationStore struct {
	evidence            delivery.AssemblyPreparationEvidence
	failRead, failRenew bool
	reads, renews       int
}

func (s *assemblyPreparationStore) ReadAssemblyPreparation(context.Context, delivery.ExpansionLease, delivery.PreparePlan, v2input.Ref) (delivery.AssemblyPreparationEvidence, error) {
	s.reads++
	if s.failRead {
		return delivery.AssemblyPreparationEvidence{}, errors.New("stale root")
	}
	return s.evidence, nil
}
func (s *assemblyPreparationStore) Renew(context.Context, delivery.ExpansionLease) error {
	s.renews++
	if s.failRenew {
		return errors.New("lost lease")
	}
	return nil
}
