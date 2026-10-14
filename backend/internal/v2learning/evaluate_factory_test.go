package v2learning

import "testing"

func TestEvaluateOrchestratorRequiresInstalledDependencies(t *testing.T) {
	for _, runtime := range []TrainingFitRuntime{
		{},
		{Bucket: "photo"},
	} {
		orchestrator, err := runtime.EvaluateOrchestrator()
		if err == nil || orchestrator.Source != nil || orchestrator.Leases != nil ||
			orchestrator.Stager != nil || orchestrator.Committer != nil ||
			orchestrator.TransportForLease != nil {
			t.Fatal("incomplete V23 Evaluate runtime installed")
		}
	}
}
