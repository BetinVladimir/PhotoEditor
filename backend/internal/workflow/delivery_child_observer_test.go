package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDeliveryStageInventoryRejectsSubstitutionAndOwnsResult(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	base := []v2input.Entry{{Ref: ref, Contract: "WorkingImage"}}
	for _, scenario := range []string{"same", "new", "sha", "contract", "role"} {
		t.Run(scenario, func(t *testing.T) {
			output := DeliveryStageOutput{Ref: ref, Role: "working_image"}
			other := []DeliveryStageOutput{
				{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}, Role: "derivative_report"},
				{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}, Role: "working_rgb"},
			}
			switch scenario {
			case "new":
				output.Ref.ArtifactID = uuid.New()
			case "sha":
				output.Ref.SHA256 = strings.Repeat("b", 64)
			case "contract":
				output.Role = "working_rgb"
			case "role":
				output.Role = "unknown"
			}
			result, err := deliveryStageEntries(base, DeliveryStageResult{Outputs: append([]DeliveryStageOutput{output}, other...)})
			if scenario == "sha" || scenario == "contract" || scenario == "role" {
				if err == nil {
					t.Fatal("substitution accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 3
			if scenario == "new" {
				want = 4
			}
			if len(result) != want {
				t.Fatal(result)
			}
			result[0].Contract = "mutated"
			if base[0].Contract != "WorkingImage" {
				t.Fatal("borrowed source inventory")
			}
		})
	}
}

func TestDeliveryStageInventoryRequiresCompleteUniqueRoles(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	valid := []DeliveryStageOutput{
		{Ref: ref(), Role: "working_image"}, {Ref: ref(), Role: "derivative_report"}, {Ref: ref(), Role: "working_rgb"},
	}
	for _, outputs := range [][]DeliveryStageOutput{
		valid[:2],
		append(append([]DeliveryStageOutput(nil), valid...), valid[0]),
		{{Ref: valid[0].Ref, Role: "working_image"}, {Ref: valid[0].Ref, Role: "derivative_report"}, valid[2]},
		{{Ref: valid[0].Ref, Role: "working_image"}, {Ref: valid[1].Ref, Role: "derivative_report"}, {Ref: valid[2].Ref, Role: "unknown"}},
	} {
		if _, err := deliveryStageEntries(nil, DeliveryStageResult{Outputs: outputs}); err == nil {
			t.Fatal("accepted incomplete or aliased stage inventory", outputs)
		}
	}
}

func TestDeliveryObserverRejectsUnconfiguredAndCancelledBeforeIO(t *testing.T) {
	p := DeliveryChildPipeline{}
	if _, err := p.PendingSlots(context.Background(), delivery.ExpansionLease{}, 1); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err := p.ObserveChild(context.Background(), delivery.ExpansionLease{}, delivery.PreparePlan{}, delivery.ExportChildIntent{}); err == nil {
		t.Fatal("missing dependencies accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if changed, err := p.ObserveChild(ctx, delivery.ExpansionLease{}, delivery.PreparePlan{}, delivery.ExportChildIntent{}); changed || !errors.Is(err, context.Canceled) {
		t.Fatal(changed, err)
	}
}

func TestDeliveryContinuationRecoveryAndFailureBoundaries(t *testing.T) {
	for _, scenario := range []string{"fresh", "saved", "recovered", "activated_error", "open_error", "publish_error", "admit_error", "cancel_after_open", "admit_recovered"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
			var saved *v2input.Ref
			if scenario == "saved" || scenario == "recovered" || scenario == "activated_error" {
				copy := ref
				saved = &copy
			}
			calls := []string{}
			step := func(name string) error {
				calls = append(calls, name)
				if scenario == name+"_error" {
					return errors.New(name)
				}
				return nil
			}
			actions := deliveryContinuationActions{
				activated: func(got v2input.Ref) (bool, error) {
					if got != ref {
						t.Fatal("recovery ref substituted")
					}
					return scenario == "recovered", step("activated")
				},
				open: func() error {
					err := step("open")
					if scenario == "cancel_after_open" {
						cancel()
					}
					return err
				},
				publish: func() (v2input.Ref, error) { return ref, step("publish") },
				admit: func(got v2input.Ref) (bool, error) {
					if got != ref {
						t.Fatal("admission ref substituted")
					}
					return scenario == "admit_recovered", step("admit")
				},
			}
			recovered, err := resumeDeliveryContinuation(ctx, saved, actions)
			want := []string{"open", "publish", "admit"}
			switch scenario {
			case "saved":
				want = []string{"activated", "open", "admit"}
			case "recovered", "activated_error":
				want = []string{"activated"}
			case "open_error", "cancel_after_open":
				want = []string{"open"}
			case "publish_error":
				want = []string{"open", "publish"}
			}
			if strings.Join(calls, ",") != strings.Join(want, ",") {
				t.Fatal("side effect order", calls, want)
			}
			failed := strings.HasSuffix(scenario, "_error") || scenario == "cancel_after_open"
			if (err != nil) != failed {
				t.Fatal(err)
			}
			if recovered != (scenario == "recovered" || scenario == "admit_recovered") {
				t.Fatal("recovery credit", recovered)
			}
		})
	}
}
