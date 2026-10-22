package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func stageNames(plan []ApplyV4Stage) []string {
	out := make([]string, len(plan))
	for i := range plan {
		out[i] = plan[i].Phase
	}
	return out
}

func TestCompileApplyV4IdentityPlan(t *testing.T) {
	plan, err := CompileApplyV4Plan(json.RawMessage(`{"mode":"fixed","local":{"enabled":false},"beauty":{"enabled":false},"defects":{"enabled":false},"geometry":{"enabled":false},"extensions":{}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"resolve", "color", "local", "quality"}
	if got := stageNames(plan); len(got) != len(want) {
		t.Fatalf("plan=%v", got)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("plan=%v", got)
			}
		}
	}
}

func TestCompileApplyV4FullPlanHasOrderedBarriers(t *testing.T) {
	raw := json.RawMessage(`{"mode":"adaptive","local":{"enabled":true},"beauty":{"enabled":true},"defects":{"enabled":true,"policy":"review"},"geometry":{"enabled":true},"extensions":{"series":{"enabled":true},"composition":{"enabled":true},"portrait_detail":{"enabled":true},"restoration":{"denoise":"scunet_color_real_psnr","upscale":"2x"},"background":{"mode":"generate"},"inpainting":{"enabled":true},"generation":{"enabled":true}}}`)
	plan, err := CompileApplyV4Plan(raw, 8)
	if err != nil {
		t.Fatal(err)
	}
	index := map[string]int{}
	for i, s := range plan {
		index[s.Phase] = i
	}
	ordered := []string{"mask", "stats", "resolve", "denoise", "pose", "geometry", "mask_transform", "color", "series_measure", "series_pack", "series_balance", "local", "face_detect", "face_landmarks", "face_parse", "face_skin", "beauty", "portrait_detail", "defects_propose", "defects_review", "background", "background_review", "inpaint", "inpaint_review", "generate", "generation_review", "composition_structure", "composition", "composition_render", "composition_review", "upscale", "quality"}
	for i, name := range ordered {
		if index[name] != i {
			t.Fatalf("stage %s index=%d plan=%v", name, index[name], stageNames(plan))
		}
	}
	if plan[index["series_balance"]].Cardinality != "aggregate" || plan[index["defects_review"]].Cardinality != "review" || plan[index["composition_review"]].Cardinality != "review" {
		t.Fatal("barrier cardinality lost")
	}
}

func TestCompileApplyV4SingletonSkipsSeriesAggregate(t *testing.T) {
	plan, err := CompileApplyV4Plan(json.RawMessage(`{"mode":"fixed","local":{"enabled":false},"beauty":{"enabled":false},"defects":{"enabled":false},"geometry":{"enabled":false},"extensions":{"series":{"enabled":true}}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan {
		if s.Phase == "series_measure" || s.Phase == "series_balance" {
			t.Fatal("singleton series must be identity")
		}
	}
}

func TestCompileApplyV4IncludesReviewedSensorDust(t *testing.T) {
	plan, err := CompileApplyV4Plan(json.RawMessage(`{"mode":"fixed","extensions":{"restoration":{"sensor_dust":"review"}}}`), 5)
	if err != nil {
		t.Fatal(err)
	}
	phases := map[string]ApplyV4Stage{}
	for _, stage := range plan {
		phases[stage.Phase] = stage
	}
	if phases["dust_detect"].Cardinality != "group" || phases["dust_review"].Cardinality != "review" || phases["dust_repair"].Operation != "sensor.dust_repair" {
		t.Fatal(plan)
	}
	auto, autoErr := CompileApplyV4Plan(json.RawMessage(`{"mode":"fixed","extensions":{"restoration":{"sensor_dust":"conservative_auto"}}}`), 10)
	if autoErr != nil {
		t.Fatal(autoErr)
	}
	found := false
	for _, stage := range auto {
		if stage.Phase == "dust_review" {
			t.Fatal("automatic dust must not impersonate review")
		}
		if stage.Operation == "sensor.dust_auto_repair" {
			found = true
		}
	}
	if !found {
		t.Fatal("automatic dust repair omitted")
	}
}

func TestEffectiveApplyV4StyleIncludesOverrideStages(t *testing.T) {
	style := json.RawMessage(`{"mode":"fixed","local":{"enabled":false},"beauty":{"enabled":false},"defects":{"enabled":false},"geometry":{"enabled":false},"extensions":{}}`)
	override := json.RawMessage(`{"modules":{"beauty":{"enabled":true},"geometry":{"enabled":true},"extensions":{"composition":{"enabled":true},"restoration":{"denoise":"off","upscale":"2x"}}}}`)
	effective, err := EffectiveApplyV4Style(style, override)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompileApplyV4Plan(effective, 1)
	if err != nil {
		t.Fatal(err)
	}
	names := stageNames(plan)
	for _, want := range []string{"geometry", "beauty", "composition_structure", "composition_render", "composition_review", "upscale"} {
		found := false
		for _, name := range names {
			found = found || name == want
		}
		if !found {
			t.Fatalf("stage %s missing from %v", want, names)
		}
	}
}

func TestCompileApplyV4BackgroundRequiresSubjectMask(t *testing.T) {
	plan, err := CompileApplyV4Plan(json.RawMessage(`{"mode":"fixed","extensions":{"background":{"mode":"transparent"}}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mask", "resolve", "color", "local", "background", "quality"}
	got := stageNames(plan)
	if len(got) != len(want) {
		t.Fatalf("plan=%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("plan=%v", got)
		}
	}
}

func TestCompileApplyV4BlurPredictsDepthOnFinalPixels(t *testing.T) {
	plan, err := CompileApplyV4Plan(json.RawMessage(`{"mode":"fixed","extensions":{"background":{"mode":"blur"}}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mask", "resolve", "color", "local", "depth", "background", "quality"}
	got := stageNames(plan)
	if len(got) != len(want) {
		t.Fatalf("plan=%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("plan=%v", got)
		}
	}
}

func TestApplyV4PlanCanonicalDocumentIsStable(t *testing.T) {
	raw := json.RawMessage(`{"mode":"fixed","local":{"enabled":false},"beauty":{"enabled":false},"defects":{"enabled":false},"geometry":{"enabled":false},"extensions":{}}`)
	a, _ := CompileApplyV4Plan(raw, 2)
	b, _ := CompileApplyV4Plan(raw, 2)
	left, err := canonical.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	right, _ := canonical.Marshal(b)
	if string(left) != string(right) || fmt.Sprintf("%x", sha256.Sum256(left)) != fmt.Sprintf("%x", sha256.Sum256(right)) {
		t.Fatal("v4 plan is not deterministic")
	}
}

func TestEffectiveApplyV4StyleKeepsUnnamedExtensionModules(t *testing.T) {
	style := json.RawMessage(`{"mode":"fixed","extensions":{"restoration":{"denoise":"off","upscale":"2x"},"region_adjustments":[]}}`)
	override := json.RawMessage(`{"modules":{"extensions":{"region_adjustments":[]}}}`)
	effective, err := EffectiveApplyV4Style(style, override)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Extensions struct {
			Restoration struct {
				Upscale string `json:"upscale"`
			} `json:"restoration"`
		} `json:"extensions"`
	}
	if err = json.Unmarshal(effective, &value); err != nil {
		t.Fatal(err)
	}
	if value.Extensions.Restoration.Upscale != "2x" {
		t.Fatal(string(effective))
	}
}

func TestEffectiveApplyV4JobThenItemModules(t *testing.T) {
	style := json.RawMessage(`{"mode":"fixed","local":{"enabled":false},"beauty":{"enabled":false},"defects":{"enabled":false},"geometry":{"enabled":false},"extensions":{}}`)
	job := json.RawMessage(`{"schema_version":1,"modules":{"local":{"enabled":true},"extensions":{"restoration":{"upscale":"2x"}}}}`)
	item := json.RawMessage(`{"modules":{"local":{"enabled":false},"extensions":{"region_adjustments":[]}}}`)
	effective, err := EffectiveApplyV4Style(style, job)
	if err != nil {
		t.Fatal(err)
	}
	effective, err = EffectiveApplyV4Style(effective, item)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Local struct {
			Enabled bool `json:"enabled"`
		} `json:"local"`
		Extensions struct {
			Restoration struct {
				Upscale string `json:"upscale"`
			} `json:"restoration"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal(effective, &result); err != nil {
		t.Fatal(err)
	}
	if result.Local.Enabled || result.Extensions.Restoration.Upscale != "2x" {
		t.Fatalf("wrong merged modules: %s", effective)
	}
}

func TestEffectiveApplyV4NullModuleInherits(t *testing.T) {
	style := json.RawMessage(`{"mode":"fixed","local":{"enabled":true},"geometry":{"enabled":true},"extensions":{"restoration":{"upscale":"2x"}}}`)
	override := json.RawMessage(`{"modules":{"local":null,"geometry":null,"extensions":null}}`)
	got, err := EffectiveApplyV4Style(style, override)
	if err != nil {
		t.Fatal(err)
	}
	want, err := canonical.Bytes(style)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("null changed inherited modules: %s", got)
	}
	if _, err := EffectiveApplyV4Style(json.RawMessage(`null`), override); err == nil {
		t.Fatal("null style accepted")
	}
}

func TestConservativeDefectsHaveDedicatedPinnedRepairWithoutReview(t *testing.T) {
	plan, err := CompileApplyV4Plan(json.RawMessage(`{"mode":"fixed","defects":{"enabled":true,"policy":"conservative_auto"}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal, repair := -1, -1
	for index, stage := range plan {
		if stage.Phase == "defects_review" {
			t.Fatal("automatic policy reused human review")
		}
		if stage.Operation == "defects.propose" {
			proposal = index
		}
		if stage.Operation == "defects.auto_repair" {
			repair = index
		}
	}
	if proposal < 0 || repair != proposal+1 {
		t.Fatalf("automatic repair order: %+v", plan)
	}
}

func TestConservativeProfileAdmitsNativePeakAndRejectsUnboundedDimensions(t *testing.T) {
	raw, err := applyConservativeProfile(24000000, 288000000)
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Backend  string           `json:"backend"`
		Estimate ResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(raw, &profile) != nil || profile.Backend != "cpu" || profile.Estimate.RAMBytes < 24000000*128+(256<<20) {
		t.Fatalf("profile: %s", raw)
	}
	if _, err = applyConservativeProfile(200000001, 1); err == nil {
		t.Fatal("pixel bound not enforced")
	}
}
