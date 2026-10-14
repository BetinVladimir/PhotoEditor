package v2optical

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiTaskPlanAndExactInputInventory(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
			qualified, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			capacity, err := MultiResources(operation, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := CompileMultiTaskPlan(context.Background(), operation, params, originals, qualified, capacity)
			if err != nil {
				t.Fatal(err)
			}
			published := v2input.Ref{ArtifactID: uuid.New(), SHA256: plan.ParamsSHA}
			entries, direct, err := MultiTaskInputEntries(context.Background(), plan, originals, qualified, published)
			if err != nil || len(entries) != 7 || len(direct) != 5 || len(plan.PhysicalImageOccurrences) != 2 {
				t.Fatalf("%v entries=%d direct=%d", err, len(entries), len(direct))
			}
			counts := map[string]int{}
			for i, entry := range entries {
				counts[entry.Contract]++
				if i > 0 && entries[i-1].Ref.ArtifactID.String() >= entry.Ref.ArtifactID.String() {
					t.Fatal("unordered transport entries")
				}
			}
			if counts["WorkingImage"] != 2 || counts["NpyArray"] != 2 || counts["Params"] != 1 {
				t.Fatal(counts)
			}
			original := params[0]
			params[0] = ' '
			if plan.Params[0] != original {
				t.Fatal("plan aliases caller Params")
			}
		})
	}
}
func TestMultiTaskPlanMutationRejected(t *testing.T) {
	for _, fault := range []string{"operation", "digest", "reserve", "direct", "physical", "occurrence", "profile", "published"} {
		t.Run(fault, func(t *testing.T) {
			operation := "optical.panorama"
			pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
			qualified, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			capacity, err := MultiResources(operation, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := CompileMultiTaskPlan(context.Background(), operation, params, originals, qualified, capacity)
			if err != nil {
				t.Fatal(err)
			}
			published := v2input.Ref{ArtifactID: uuid.New(), SHA256: plan.ParamsSHA}
			switch fault {
			case "operation":
				plan.Operation = "optical.focus_stack"
			case "digest":
				plan.ParamsSHA = pin.DatasetSHA
			case "reserve":
				plan.ReservedBytes++
			case "direct":
				plan.DirectInputs = plan.DirectInputs[1:]
			case "physical":
				plan.PhysicalImageInputs[0].Contract = "Report"
			case "occurrence":
				plan.PhysicalImageOccurrences[0].Ordinal = 1
			case "profile":
				plan.Profile = append([]byte(" "), plan.Profile...)
			case "published":
				published.ArtifactID = pin.Report.ArtifactID
			}
			if _, _, err := MultiTaskInputEntries(context.Background(), plan, originals, qualified, published); err == nil {
				t.Fatal("modified plan accepted")
			}
		})
	}
}
func TestMultiTaskPlanCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompileMultiTaskPlan(ctx, "optical.panorama", nil, nil, VerifiedMultiQualification{}, MultiResourceEstimate{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
