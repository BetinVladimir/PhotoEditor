package v2region

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"photoeditor/backend/internal/canonical"
)

type regionQualificationFunc func(context.Context, FrozenRegionPlan) (RegionQualification, error)

func (f regionQualificationFunc) ResolveRegionQualification(ctx context.Context, p FrozenRegionPlan) (RegionQualification, error) {
	return f(ctx, p)
}
func regionCalibrationFixture(t *testing.T, plan FrozenRegionPlan) RegionCalibration {
	t.Helper()
	workload, err := RegionWorkloadSHA(plan)
	if err != nil {
		t.Fatal(err)
	}
	shape, _ := plan.SourceShape()
	doc, _ := plan.decode()
	return RegionCalibration{Qualification: RegionQualification{RuntimeSHA: regionSHA, ModelSHA: doc.FrozenRecipe.ModelLockSHA, ICCSHA: doc.FrozenRecipe.ICCLockSHA,
		DeviceSHA: strings.Repeat("b", 64), DatasetSHA: strings.Repeat("c", 64), PolicySHA: strings.Repeat("d", 64), Backend: "cuda"},
		WorkloadSHA: workload, EvidenceSHA: strings.Repeat("e", 64), Samples: []RegionCalibrationSample{
			{Width: shape.Width * 2, Height: shape.Height * 2, Peak: RegionOperatorPeak{RAMBytes: 1000, ScratchBytes: 2000, VRAMBytes: 3000, OutputBytes: 4000}},
			{Width: shape.Width, Height: shape.Height, Peak: RegionOperatorPeak{RAMBytes: 100, ScratchBytes: 200, VRAMBytes: 300, OutputBytes: 400}},
		}}
}
func regionCalibrationBytes(t *testing.T, profiles []RegionCalibration) ([]byte, string) {
	t.Helper()
	raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "profiles": profiles})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}
func TestRegionCalibrationSelectedWholeCanvasAndRetainsAllPeaks(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	p := regionCalibrationFixture(t, plan)
	raw, pin := regionCalibrationBytes(t, []RegionCalibration{p})
	catalog, err := NewPinnedRegionCalibrationCatalog(raw, pin)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'X'
	p.Samples[1].Peak.VRAMBytes = 0
	cost, err := catalog.Select(context.Background(), p.Qualification, plan)
	if err != nil || cost.RAMBytes != 100 || cost.ScratchBytes != 200 || cost.VRAMBytes != 300 || cost.OutputBytes != 400 {
		t.Fatal("wrong measured peak", cost, err)
	}
	// A larger covering sample remains fully reserved even on a smaller canvas.
	p = regionCalibrationFixture(t, plan)
	p.Samples = p.Samples[:1]
	raw, pin = regionCalibrationBytes(t, []RegionCalibration{p})
	catalog, err = NewPinnedRegionCalibrationCatalog(raw, pin)
	if err != nil {
		t.Fatal(err)
	}
	cost, err = catalog.Select(context.Background(), p.Qualification, plan)
	if err != nil || cost.ScratchBytes != 2000 || cost.OutputBytes != 4000 {
		t.Fatal("unmeasured downscaling", cost, err)
	}
	provider := CompiledRegionProfiles{Qualifier: CalibratedRegionCosts{Catalog: catalog, Resolver: regionQualificationFunc(func(context.Context, FrozenRegionPlan) (RegionQualification, error) { return p.Qualification, nil })}}
	policy, err := provider.CompileRegionProfile(context.Background(), plan)
	if err != nil || policy.PolicySHA != p.Qualification.PolicySHA {
		t.Fatal("catalog/compiler wiring", policy, err)
	}
}
func TestRegionCalibrationDoesNotExtrapolateRotateOrSubstitutePolicy(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	p := regionCalibrationFixture(t, plan)
	shape, _ := plan.SourceShape()
	p.Samples = []RegionCalibrationSample{{Width: shape.Width - 1, Height: shape.Height * 2, Peak: RegionOperatorPeak{VRAMBytes: 1}}}
	raw, pin := regionCalibrationBytes(t, []RegionCalibration{p})
	c, err := NewPinnedRegionCalibrationCatalog(raw, pin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Select(context.Background(), p.Qualification, plan); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("noncovering measurement accepted", err)
	}
	p = regionCalibrationFixture(t, plan)
	raw, pin = regionCalibrationBytes(t, []RegionCalibration{p})
	c, _ = NewPinnedRegionCalibrationCatalog(raw, pin)
	q := p.Qualification
	q.DeviceSHA = strings.Repeat("f", 64)
	if _, err = c.Select(context.Background(), q, plan); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("device substituted", err)
	}
	q = p.Qualification
	q.Backend = "cpu"
	if _, err = c.Select(context.Background(), q, plan); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("CPU substituted", err)
	}
	p.WorkloadSHA = strings.Repeat("f", 64)
	raw, pin = regionCalibrationBytes(t, []RegionCalibration{p})
	c, _ = NewPinnedRegionCalibrationCatalog(raw, pin)
	if _, err = c.Select(context.Background(), p.Qualification, plan); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("different style accepted", err)
	}
}
func TestRegionCalibrationRejectsAmbiguousOrUnmeasuredManifest(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	p := regionCalibrationFixture(t, plan)
	raw, pin := regionCalibrationBytes(t, []RegionCalibration{p})
	if _, err := NewPinnedRegionCalibrationCatalog(raw, strings.Repeat("0", 64)); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal(err)
	}
	variants := []func(map[string]any){
		func(m map[string]any) { m["schema_version"] = 2 },
		func(m map[string]any) { m["extra"] = true },
		func(m map[string]any) {
			m["profiles"].([]any)[0].(map[string]any)["samples"].([]any)[0].(map[string]any)["operator_peak"].(map[string]any)["vram_bytes"] = 0
		},
		func(m map[string]any) {
			delete(m["profiles"].([]any)[0].(map[string]any)["samples"].([]any)[0].(map[string]any)["operator_peak"].(map[string]any), "ram_bytes")
		},
		func(m map[string]any) {
			m["profiles"].([]any)[0].(map[string]any)["samples"].([]any)[0].(map[string]any)["operator_peak"].(map[string]any)["ram_bytes"] = nil
		},
		func(m map[string]any) {
			m["profiles"].([]any)[0].(map[string]any)["samples"].([]any)[0].(map[string]any)["operator_peak"].(map[string]any)["ram_bytes"] = true
		},
		func(m map[string]any) {
			m["profiles"].([]any)[0].(map[string]any)["samples"].([]any)[0].(map[string]any)["operator_peak"].(map[string]any)["ram_bytes"] = -1
		},
		func(m map[string]any) {
			m["profiles"].([]any)[0].(map[string]any)["qualification"].(map[string]any)["Backend"] = "cuda"
		},
	}
	for i, change := range variants {
		var m map[string]any
		json.Unmarshal(raw, &m)
		change(m)
		altered, _ := canonical.Marshal(m)
		sum := sha256.Sum256(altered)
		if _, err := NewPinnedRegionCalibrationCatalog(altered, hex.EncodeToString(sum[:])); !errors.Is(err, ErrRegionInvalid) {
			t.Fatal("unmeasured field", i, err)
		}
	}
	duplicate, duplicatePin := regionCalibrationBytes(t, []RegionCalibration{p, p})
	if _, err := NewPinnedRegionCalibrationCatalog(duplicate, duplicatePin); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal("duplicate qualification/style accepted", err)
	}
	spaced := append([]byte(" "), raw...)
	sum := sha256.Sum256(spaced)
	if _, err := NewPinnedRegionCalibrationCatalog(spaced, hex.EncodeToString(sum[:])); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal("noncanonical accepted", err)
	}
	if _, err := NewPinnedRegionCalibrationCatalog(raw, pin); err != nil {
		t.Fatal(err)
	}
}
func TestRegionCalibrationBoundedFileRejectsLinksAndFIFO(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	raw, pin := regionCalibrationBytes(t, []RegionCalibration{regionCalibrationFixture(t, plan)})
	dir := t.TempDir()
	path := filepath.Join(dir, "calibration.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPinnedRegionCalibrationCatalog(path, pin); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "symlink")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPinnedRegionCalibrationCatalog(link, pin); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal("symlink accepted", err)
	}
	hard := filepath.Join(dir, "hardlink")
	if err := os.Link(path, hard); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPinnedRegionCalibrationCatalog(path, pin); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal("hardlink accepted", err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPinnedRegionCalibrationCatalog(fifo, pin); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal("FIFO accepted", err)
	}
}
func TestRegionCalibratedCostCancellationAndUnavailableResolver(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	p := regionCalibrationFixture(t, plan)
	raw, pin := regionCalibrationBytes(t, []RegionCalibration{p})
	c, _ := NewPinnedRegionCalibrationCatalog(raw, pin)
	if _, err := (CalibratedRegionCosts{Catalog: c}).QualifyRegionCost(context.Background(), plan); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	costs := CalibratedRegionCosts{Catalog: c, Resolver: regionQualificationFunc(func(context.Context, FrozenRegionPlan) (RegionQualification, error) {
		cancel()
		return p.Qualification, nil
	})}
	if _, err := costs.QualifyRegionCost(ctx, plan); !errors.Is(err, context.Canceled) {
		t.Fatal("post resolver cancellation lost", err)
	}
}

func TestRegionCalibrationTieBreakIndependentOfRegistryOrder(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	p := regionCalibrationFixture(t, plan)
	p.Samples = []RegionCalibrationSample{
		{Width: 4000, Height: 2000, Peak: RegionOperatorPeak{VRAMBytes: 400}},
		{Width: 2000, Height: 4000, Peak: RegionOperatorPeak{VRAMBytes: 200}},
	}
	for i := 0; i < 2; i++ {
		raw, pin := regionCalibrationBytes(t, []RegionCalibration{p})
		c, err := NewPinnedRegionCalibrationCatalog(raw, pin)
		if err != nil {
			t.Fatal(err)
		}
		cost, err := c.Select(context.Background(), p.Qualification, plan)
		if err != nil || cost.VRAMBytes != 200 {
			t.Fatal("unstable covering sample", cost, err)
		}
		p.Samples[0], p.Samples[1] = p.Samples[1], p.Samples[0]
	}
}
