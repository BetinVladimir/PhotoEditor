package styleplan

import (
	"encoding/json"
	"testing"
)

func mustCompile(t *testing.T, raw string, count int) []Stage {
	t.Helper()
	plan, err := Compile(json.RawMessage(raw), count)
	if err != nil {
		t.Fatalf("unexpected compile error for %q: %v", raw, err)
	}
	return plan
}

func hasPhase(plan []Stage, phase string) bool {
	for _, s := range plan {
		if s.Phase == phase {
			return true
		}
	}
	return false
}

// ── invalid inputs ────────────────────────────────────────────────────────────

func TestCompileRejectsZeroItemCount(t *testing.T) {
	if _, err := Compile(json.RawMessage(`{"mode":"fixed"}`), 0); err == nil {
		t.Fatal("itemCount 0 must be rejected")
	}
}

func TestCompileRejectsTooManyItems(t *testing.T) {
	if _, err := Compile(json.RawMessage(`{"mode":"fixed"}`), 5001); err == nil {
		t.Fatal("itemCount 5001 must be rejected")
	}
}

func TestCompileRejectsInvalidMode(t *testing.T) {
	if _, err := Compile(json.RawMessage(`{"mode":"streaming"}`), 1); err == nil {
		t.Fatal("mode 'streaming' must be rejected")
	}
}

func TestCompileRejectsInvalidDefectsPolicy(t *testing.T) {
	if _, err := Compile(json.RawMessage(`{"mode":"fixed","defects":{"enabled":true,"policy":"erase_all"}}`), 1); err == nil {
		t.Fatal("unknown defects policy must be rejected")
	}
}

func TestCompileRejectsInvalidBackgroundMode(t *testing.T) {
	if _, err := Compile(json.RawMessage(`{"mode":"fixed","extensions":{"background":{"mode":"neon"}}}`), 1); err == nil {
		t.Fatal("unknown background mode must be rejected")
	}
}

// ── adaptive mode ─────────────────────────────────────────────────────────────

func TestCompileAdaptiveModeProducesMaskAndStats(t *testing.T) {
	plan := mustCompile(t, `{"mode":"adaptive"}`, 1)
	if !hasPhase(plan, "mask") || !hasPhase(plan, "stats") {
		t.Fatal("adaptive mode must produce mask and stats stages")
	}
}

// ── beauty ────────────────────────────────────────────────────────────────────

func TestCompileBeautyProducesFaceAndBeautyStages(t *testing.T) {
	plan := mustCompile(t, `{"mode":"fixed","beauty":{"enabled":true}}`, 1)
	if !hasPhase(plan, "face_detect") || !hasPhase(plan, "beauty") {
		t.Fatal("beauty must produce face_detect and beauty stages")
	}
}

// ── defects conservative_auto ─────────────────────────────────────────────────

func TestCompileDefectsConservativeAutoProducesAutoRepair(t *testing.T) {
	plan := mustCompile(t, `{"mode":"fixed","defects":{"enabled":true,"policy":"conservative_auto"}}`, 1)
	if !hasPhase(plan, "defects_propose") || !hasPhase(plan, "defects_auto_repair") {
		t.Fatal("conservative_auto defects must produce auto_repair stage")
	}
	if hasPhase(plan, "defects_review") {
		t.Fatal("conservative_auto must not add review stage")
	}
}

// ── background modes ──────────────────────────────────────────────────────────

func TestCompileBackgroundBlurProducesDepthStage(t *testing.T) {
	plan := mustCompile(t, `{"mode":"fixed","extensions":{"background":{"mode":"blur"}}}`, 1)
	if !hasPhase(plan, "depth") || !hasPhase(plan, "background") {
		t.Fatal("background blur must produce depth and background stages")
	}
}

func TestCompileBackgroundAssetProducesInspectAndDecode(t *testing.T) {
	plan := mustCompile(t, `{"mode":"fixed","extensions":{"background":{"mode":"asset"}}}`, 1)
	if !hasPhase(plan, "background_inspect") || !hasPhase(plan, "background_decode") || !hasPhase(plan, "background") {
		t.Fatal("background asset must produce inspect, decode and background stages")
	}
}

func TestCompileBackgroundGenerateProducesReview(t *testing.T) {
	plan := mustCompile(t, `{"mode":"fixed","extensions":{"background":{"mode":"generate"}}}`, 1)
	if !hasPhase(plan, "background") || !hasPhase(plan, "background_review") {
		t.Fatal("background generate must produce background and review stages")
	}
}

func TestCompileBackgroundTransparentNoReview(t *testing.T) {
	plan := mustCompile(t, `{"mode":"fixed","extensions":{"background":{"mode":"transparent"}}}`, 1)
	if !hasPhase(plan, "background") || hasPhase(plan, "background_review") {
		t.Fatal("background transparent must produce background stage without review")
	}
}

// ── inpainting ────────────────────────────────────────────────────────────────

func TestCompileInpaintingProducesReview(t *testing.T) {
	plan := mustCompile(t, `{"mode":"fixed","extensions":{"inpainting":{"enabled":true}}}`, 1)
	if !hasPhase(plan, "inpaint") || !hasPhase(plan, "inpaint_review") {
		t.Fatal("inpainting must produce inpaint and inpaint_review stages")
	}
}

// ── series (itemCount >= 2) ───────────────────────────────────────────────────

func TestCompileSeriesRequiresTwoItems(t *testing.T) {
	json1 := `{"mode":"fixed","extensions":{"series":{"enabled":true}}}`
	plan1 := mustCompile(t, json1, 1)
	if hasPhase(plan1, "series_measure") {
		t.Fatal("series must not fire for a single item")
	}
	plan2 := mustCompile(t, json1, 2)
	if !hasPhase(plan2, "series_measure") || !hasPhase(plan2, "series_balance") {
		t.Fatal("series must produce series_measure and series_balance for 2 items")
	}
}

// ── sensor dust with review policy ───────────────────────────────────────────

func TestCompileSensorDustReviewProducesReviewStage(t *testing.T) {
	plan := mustCompile(t, `{"mode":"fixed","extensions":{"restoration":{"sensor_dust":"review"}}}`, 1)
	if !hasPhase(plan, "dust_detect") || !hasPhase(plan, "dust_review") || !hasPhase(plan, "dust_repair") {
		t.Fatal("sensor_dust:review must produce detect, review and repair stages")
	}
}

// ── region adjustments with person/background selector ───────────────────────

func TestCompileRegionPersonSelectorRequiresMask(t *testing.T) {
	plan := mustCompile(t, `{"mode":"fixed","extensions":{"region_adjustments":[{"region_id":"r1","selector":"person"}]}}`, 1)
	if !hasPhase(plan, "mask") {
		t.Fatal("region_adjustments with 'person' selector must produce mask stage")
	}
}

func TestCompileRegionBackgroundSelectorRequiresMask(t *testing.T) {
	plan := mustCompile(t, `{"mode":"fixed","extensions":{"region_adjustments":[{"region_id":"r1","selector":"background"}]}}`, 1)
	if !hasPhase(plan, "mask") {
		t.Fatal("region_adjustments with 'background' selector must produce mask stage")
	}
}

// ── CompileAuthoredBasePlan edge cases ────────────────────────────────────────

func TestCompileAuthoredBasePlanRejectsInvalidStyle(t *testing.T) {
	if _, err := CompileAuthoredBasePlan(json.RawMessage(`{"mode":"bad"}`), 1); err == nil {
		t.Fatal("authored base plan must reject invalid style")
	}
}

func TestCompileAuthoredBasePlanRejectsMalformedJSON(t *testing.T) {
	if _, err := CompileAuthoredBasePlan(json.RawMessage(`{not json`), 1); err == nil {
		t.Fatal("authored base plan must reject malformed JSON")
	}
}
