package v2portrait

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAnalysisProfileInstallsV2TransportAndScratchBudget(t *testing.T) {
	for _, operation := range []string{"depth.predict", "mask.person"} {
		request := analysisPlanFixture(t)
		request.Operation = operation
		minimum, err := RelightAnalysisMinimumResources(request)
		if err != nil {
			t.Fatal(err)
		}
		if minimum.ScratchBytes < 32<<20 || minimum.RAMBytes < 1<<30 || minimum.PixelCount != request.Runtime.Resources.PixelCount {
			t.Fatal("inadequate resource floor")
		}
		request.Runtime.Resources = minimum
		request.Capacity = minimum
		plan, err := CompileRelightAnalysisPlan(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		var profile struct {
			RendererVersion string                `json:"renderer_version"`
			Backend         string                `json:"backend"`
			Resources       LocalResourceEstimate `json:"resource_estimate"`
		}
		if json.Unmarshal(plan.snapshot.Profile, &profile) != nil || profile.RendererVersion != "v2.2" || profile.Backend != "cpu" || profile.Resources != minimum {
			t.Fatal("worker profile incompatible")
		}
		request.Runtime.Resources.ScratchBytes--
		if _, err := CompileRelightAnalysisPlan(context.Background(), request); err == nil {
			t.Fatal("scratch underreservation accepted")
		}
	}
}
