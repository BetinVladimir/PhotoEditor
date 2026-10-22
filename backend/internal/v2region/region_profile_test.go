package v2region

import (
	"context"
	"encoding/json"
	"errors"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

type regionCostFunc func(context.Context, FrozenRegionPlan) (RegionQualifiedCost, error)

func (f regionCostFunc) QualifyRegionCost(ctx context.Context, p FrozenRegionPlan) (RegionQualifiedCost, error) {
	return f(ctx, p)
}
func regionCostFixture(t *testing.T, plan FrozenRegionPlan) RegionQualifiedCost {
	t.Helper()
	p := regionCalibrationFixture(t, plan)
	p.Qualification.PolicySHA = strings.Repeat("b", 64)
	raw, pin := regionCalibrationBytes(t, []RegionCalibration{p})
	catalog, err := NewPinnedRegionCalibrationCatalog(raw, pin)
	if err != nil {
		t.Fatal(err)
	}
	cost, err := catalog.Select(context.Background(), p.Qualification, plan)
	if err != nil {
		t.Fatal(err)
	}
	return cost
}

func TestMPSRegionProfileUsesIndependentBackendCalibration(t *testing.T) {
    plan,_,_,_:=regionNodeFixture(t)
    calibration:=regionCalibrationFixture(t,plan)
    calibration.Qualification.Backend="mps"
    raw,pin:=regionCalibrationBytes(t,[]RegionCalibration{calibration})
    catalog,err:=NewPinnedRegionCalibrationCatalog(raw,pin)
    if err!=nil {t.Fatal(err)}
    cost,err:=catalog.Select(context.Background(),calibration.Qualification,plan)
    if err!=nil {t.Fatal(err)}
    policy,err:=CompileRegionExecutionPolicy(plan,cost)
    if err!=nil {t.Fatal(err)}
    var profile struct {Backend string `json:"backend"`;Pin RegionCalibrationPin `json:"region_qualification"`}
    if err=json.Unmarshal(policy.Profile,&profile);err!=nil {t.Fatal(err)}
    if profile.Backend!="mps" || profile.Pin.Qualification!=calibration.Qualification {t.Fatal("backend pin rewritten",profile)}
    foreign:=calibration.Qualification;foreign.Backend="cuda"
    if _,err=catalog.Select(context.Background(),foreign,plan);err==nil {t.Fatal("MPS catalog reused as CUDA calibration")}
}

func TestCompiledRegionPolicyBindsPlanAndCompleteResources(t *testing.T) {
	plan, task, params, _ := regionNodeFixture(t)
	policy, err := CompileRegionExecutionPolicy(plan, regionCostFixture(t, plan))
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Backend, AlphaMode string
		Runtime            string                 `json:"region_runtime_sha256"`
		Resources          RegionResourceEstimate `json:"resource_estimate"`
		Qualification      RegionCalibrationPin   `json:"region_qualification"`
	}
	if err = json.Unmarshal(policy.Profile, &profile); err != nil {
		t.Fatal(err)
	}
	shape, _ := plan.SourceShape()
	source, _ := shape.ResourceBudget()
	document, _ := plan.decode()
	roi := document.Geometry.OutputWidth * document.Geometry.OutputHeight
	if profile.Qualification != regionCostFixture(t, plan).Pin || profile.Backend != "cuda" || profile.Runtime != regionSHA || profile.Resources.PixelCount != shape.Width*shape.Height ||
		profile.Resources.RAMBytes != max(int64(3)<<30, source.RAMBytes+16*source.PixelCount+64*roi)+100 ||
		profile.Resources.ScratchBytes != source.ScratchBytes+16*source.PixelCount+5*roi+26*1024*1024+200 ||
		profile.Resources.VRAMBytes != 300 || policy.ReservedBytes != 24*1024*1024+400 ||
		policy.ReservedBytes != profile.Resources.OutputBytes || policy.PolicySHA != strings.Repeat("b", 64) {
		t.Fatal("complete source/operator/presentation costs or pins missing", profile, policy)
	}
	if err = validateRegionSourceBudget(policy.Profile, shape); err != nil {
		t.Fatal(err)
	}
	if err = validateRegionArtifactBudget(policy.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err = plan.CompileNode(task, params, plan.InputRequirements(), policy.Profile); err != nil {
		t.Fatal("compiled profile not admitted by node", err)
	}
}

func TestRegionPolicyRejectsUnqualifiedOrUnsafeCost(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	changes := []func(*RegionQualifiedCost){
		func(c *RegionQualifiedCost) { c.Pin = RegionCalibrationPin{} },
		func(c *RegionQualifiedCost) { c.Pin.WorkloadSHA = strings.Repeat("0", 64) },
		func(c *RegionQualifiedCost) { c.Pin.Sample.Width = 1 },
		func(c *RegionQualifiedCost) { c.Pin.Qualification.DeviceSHA = "" },
		func(c *RegionQualifiedCost) { c.Pin.CatalogSHA = "" },
		func(c *RegionQualifiedCost) { c.Pin.CalibrationSHA = "" },
		func(c *RegionQualifiedCost) { c.Pin.EvidenceSHA = "" },
		func(c *RegionQualifiedCost) { c.RuntimeSHA = "" },
		func(c *RegionQualifiedCost) { c.PolicySHA = strings.Repeat("A", 64) },
		func(c *RegionQualifiedCost) { c.ModelLockSHA = strings.Repeat("b", 64) },
		func(c *RegionQualifiedCost) { c.ICCLockSHA = strings.Repeat("b", 64) },
		func(c *RegionQualifiedCost) { c.RAMBytes = -1 },
		func(c *RegionQualifiedCost) { c.ScratchBytes = -1 },
		func(c *RegionQualifiedCost) { c.VRAMBytes = 0 },
		func(c *RegionQualifiedCost) { c.OutputBytes = -1 },
		func(c *RegionQualifiedCost) { c.RAMBytes = 9007199254740991 },
		func(c *RegionQualifiedCost) { c.ScratchBytes = 9007199254740991 },
		func(c *RegionQualifiedCost) { c.OutputBytes = 9007199254740991 },
		func(c *RegionQualifiedCost) { c.VRAMBytes = 9007199254740992 },
	}
	for i, change := range changes {
		c := regionCostFixture(t, plan)
		change(&c)
		result, err := CompileRegionExecutionPolicy(plan, c)
		if !errors.Is(err, ErrRegionInvalid) || len(result.Profile) != 0 || result.ReservedBytes != 0 {
			t.Fatal("unsafe cost", i, result, err)
		}
	}
	if _, err := CompileRegionExecutionPolicy(FrozenRegionPlan{}, regionCostFixture(t, plan)); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal(err)
	}
}

func TestRegionProfilesRequireQualifierAndPreserveCancellation(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	if _, err := (CompiledRegionProfiles{}).CompileRegionProfile(context.Background(), plan); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	provider := CompiledRegionProfiles{Qualifier: regionCostFunc(func(_ context.Context, owned FrozenRegionPlan) (RegionQualifiedCost, error) {
		calls++
		owned.body[0] = 'X'
		owned.inputs[0].Contract = "changed"
		cancel()
		return regionCostFixture(t, plan), nil
	})}
	before := plan.JSON()
	entry := plan.InputRequirements()[0]
	if _, err := provider.CompileRegionProfile(ctx, plan); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if string(plan.JSON()) != string(before) || plan.InputRequirements()[0] != entry {
		t.Fatal("qualifier mutated original plan")
	}
	if _, err := provider.CompileRegionProfile(ctx, plan); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled qualifier called", err, calls)
	}
	sentinel := errors.New("native operator unqualified")
	provider.Qualifier = regionCostFunc(func(context.Context, FrozenRegionPlan) (RegionQualifiedCost, error) {
		return RegionQualifiedCost{}, sentinel
	})
	if _, err := provider.CompileRegionProfile(context.Background(), plan); !errors.Is(err, sentinel) {
		t.Fatal("qualification failure hidden", err)
	}
}

func TestRegionProfilesRejectInvalidPlanBeforeQualification(t *testing.T) {
	provider := CompiledRegionProfiles{Qualifier: regionCostFunc(func(context.Context, FrozenRegionPlan) (RegionQualifiedCost, error) {
		t.Fatal("invalid plan reached qualification")
		return RegionQualifiedCost{}, nil
	})}
	if _, err := provider.CompileRegionProfile(context.Background(), FrozenRegionPlan{}); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal(err)
	}
}

func TestRegionPinnedNodeRejectsProofAndBudgetMutation(t *testing.T) {
	plan, task, params, _ := regionNodeFixture(t)
	policy, err := CompileRegionExecutionPolicy(plan, regionCostFixture(t, plan))
	if err != nil {
		t.Fatal(err)
	}
	changes := []func(map[string]any){
		func(m map[string]any) { m["resource_estimate"].(map[string]any)["ram_bytes"] = 1 },
		func(m map[string]any) { m["resource_estimate"].(map[string]any)["scratch_bytes"] = 1 },
		func(m map[string]any) {
			m["region_qualification"].(map[string]any)["workload_sha"] = strings.Repeat("0", 64)
		},
		func(m map[string]any) {
			delete(m["region_qualification"].(map[string]any)["sample"].(map[string]any)["operator_peak"].(map[string]any), "ram_bytes")
		},
		func(m map[string]any) { m["region_qualification"].(map[string]any)["extra"] = true },
		func(m map[string]any) { m["alpha_mode"] = "require_float32" },
	}
	for i, change := range changes {
		var fields map[string]any
		json.Unmarshal(policy.Profile, &fields)
		change(fields)
		raw, err := canonical.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = plan.CompileNode(task, params, plan.InputRequirements(), raw); !errors.Is(err, ErrRegionInvalid) {
			t.Fatal("proof/budget mutation", i, err)
		}
	}
	node, err := plan.CompileNode(task, params, plan.InputRequirements(), policy.Profile)
	if err != nil {
		t.Fatal(err)
	}
	var frozen regionNodeDocument
	if json.Unmarshal(node.JSON(), &frozen) != nil || string(frozen.Profile) != string(policy.Profile) {
		t.Fatal("qualification projection lost in node")
	}
}

func TestRegionCompileNodeRequiresMeasuredProof(t *testing.T) {
	plan, task, params, profile := regionNodeFixture(t)
	delete(profile, "region_qualification")
	raw, err := canonical.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = plan.CompileNode(task, params, plan.InputRequirements(), raw); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("unqualified node compiled", err)
	}
}
