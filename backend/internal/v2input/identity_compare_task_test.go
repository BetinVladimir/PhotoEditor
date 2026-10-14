package v2input

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestIdentityCompareProfileCapacityBounds(t *testing.T) {
	costs := SemanticQualifiedCosts{RAMBytes: 512 << 20, VRAMBytes: 6 << 30, ScratchBytes: 256 << 20}
	body, err := CompileIdentityCompareProfile("cuda", 6000, 4000, 900<<20, costs, 32<<30, 10<<30, 12<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	var profile sessionDescribeExecutionProfile
	if json.Unmarshal(body, &profile) != nil {
		t.Fatal("profile JSON")
	}
	e := profile.ResourceEstimate
	if profile.Backend != "cuda" || profile.RendererVersion != "v2.2" || e.PixelCount != 24000000 ||
		e.RAMBytes < (2<<30)+160*24000000+(900<<20)+costs.RAMBytes || e.VRAMBytes != costs.VRAMBytes ||
		e.ScratchBytes < (900<<20)+costs.ScratchBytes || e.OutputBytes < 4*24000000 {
		t.Fatal("incomplete identity capacity")
	}
	limits := [4]int64{e.RAMBytes, e.VRAMBytes, e.ScratchBytes, e.OutputBytes}
	for i := range limits {
		bad := limits
		bad[i]--
		if _, err := CompileIdentityCompareProfile("cuda", 6000, 4000, 900<<20, costs, bad[0], bad[1], bad[2], bad[3]); err == nil {
			t.Fatal("under-reserved dimension", i)
		}
	}
	if _, err := CompileIdentityCompareProfile("cuda", 6000, 4000, 900<<20, SemanticQualifiedCosts{}, 32<<30, 10<<30, 12<<30, 1<<30); err == nil {
		t.Fatal("unqualified GPU accepted")
	}
	cpuCosts := costs
	cpuCosts.VRAMBytes = 0
	if raw, err := CompileIdentityCompareProfile("cpu", 6000, 4000, 900<<20, cpuCosts, 32<<30, 0, 12<<30, 1<<30); err != nil || len(raw) == 0 {
		t.Fatal("CPU profile refused", err)
	}
	for _, backend := range []string{"mps", "unknown"} {
		if _, err := CompileIdentityCompareProfile(backend, 6000, 4000, 900<<20, cpuCosts, 32<<30, 0, 12<<30, 1<<30); err == nil {
			t.Fatal("unsupported backend", backend)
		}
	}
	if _, err := CompileIdentityCompareProfile("cpu", 6000, 4000, 900<<20, costs, 32<<30, 10<<30, 12<<30, 1<<30); err == nil {
		t.Fatal("CPU claimed VRAM")
	}
}

func TestIdentityCompareFrozenTaskMatchesClosure(t *testing.T) {
	p, paramsRef, docs, dataset := identityClosureFixture(t)
	workflow := uuid.New()
	closure, err := CompileIdentityCompareClosure(context.Background(), workflow, 0, p, paramsRef, docs, dataset)
	if err != nil {
		t.Fatal(err)
	}
	sizes := make([]SemanticInputSize, len(closure.Direct))
	for i, ref := range closure.Direct {
		sizes[i] = SemanticInputSize{Ref: ref, Bytes: 4096}
	}
	costs := SemanticQualifiedCosts{RAMBytes: 256 << 20, VRAMBytes: 6 << 30, ScratchBytes: 64 << 20}
	frozen, err := CompileIdentityCompareTask(context.Background(), workflow, 0, "cuda", p, paramsRef, docs,
		dataset, sizes, costs, 32<<30, 10<<30, 12<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := frozen.DraftTask()
	if err != nil || draft.Operation != "identity.compare" || draft.ResourceClass != "gpu" ||
		draft.ParamsArtifactID != paramsRef.ArtifactID || draft.ParamsSHA != paramsRef.SHA256 ||
		draft.InputDigest != closure.InputDigest || draft.NodeKey != "identity.compare:0" {
		t.Fatal("draft changed", err)
	}
	cpuCosts := costs
	cpuCosts.VRAMBytes = 0
	cpuTask, err := CompileIdentityCompareTask(context.Background(), workflow, 0, "cpu", p, paramsRef, docs,
		dataset, sizes, cpuCosts, 32<<30, 0, 12<<30, 1<<30)
	if err != nil {
		t.Fatal("CPU task", err)
	}
	cpuDraft, err := cpuTask.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	var cpuProfile sessionDescribeExecutionProfile
	if json.Unmarshal(cpuDraft.ExecutionProfile, &cpuProfile) != nil || cpuProfile.Backend != "cpu" || cpuProfile.ResourceEstimate.VRAMBytes != 0 {
		t.Fatal("CPU draft profile changed")
	}
	for _, change := range []func([]SemanticInputSize) []SemanticInputSize{
		func(values []SemanticInputSize) []SemanticInputSize { return values[:len(values)-1] },
		func(values []SemanticInputSize) []SemanticInputSize {
			values[0].Ref.SHA256 = p.ModelLockSHA
			return values
		},
	} {
		invalid := change(append([]SemanticInputSize(nil), sizes...))
		if _, err := CompileIdentityCompareTask(context.Background(), workflow, 0, "cuda", p, paramsRef, docs,
			dataset, invalid, costs, 32<<30, 10<<30, 12<<30, 1<<30); err == nil {
			t.Fatal("invalid inventory accepted")
		}
	}
}
