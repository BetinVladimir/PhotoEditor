package run

import (
	"encoding/json"
	"testing"
)

func TestSupportsApplyV4OnlyForMaterializedBuilders(t *testing.T) {
	var supported styleExecutionNeeds
	if err := json.Unmarshal([]byte(`{"mode":"adaptive","local":{"enabled":true},"extensions":{"series":{"enabled":true},"background":{"mode":"keep"},"restoration":{"denoise":"off","upscale":"off"}}}`), &supported); err != nil {
		t.Fatal(err)
	}
	if !supportsApplyV4(supported) {
		t.Fatal("supported color/local/series plan rejected")
	}
	mutations := []func(*styleExecutionNeeds){
		func(v *styleExecutionNeeds) { v.Defects.Enabled = true; v.Defects.Policy = "automatic" },
		func(v *styleExecutionNeeds) { v.Extensions.PortraitDetail.Enabled = true },
		func(v *styleExecutionNeeds) { v.Extensions.Restoration.Denoise = "unknown" }, func(v *styleExecutionNeeds) { v.Extensions.Restoration.Upscale = "8x" },
		func(v *styleExecutionNeeds) { v.Extensions.Background.Mode = "unknown" },
	}
	for i, mutate := range mutations {
		candidate := supported
		mutate(&candidate)
		if supportsApplyV4(candidate) {
			t.Fatalf("unsupported module %d activated v4", i)
		}
	}
}

func TestApplyV4CreativeStagesRequireExplicitTargetMasks(t *testing.T) {
	var style styleExecutionNeeds
	if err := json.Unmarshal([]byte(`{"mode":"fixed","extensions":{"inpainting":{"enabled":true,"model":"big_lama","policy":"review","mask_source":"per_image"},"generation":{"enabled":true,"model":"sd15_inpaint","prompt":"fill","negative_prompt":"","steps":25,"guidance_scale":7.5,"require_acceptance":true},"background":{"mode":"keep"}}}`), &style); err != nil {
		t.Fatal(err)
	}
	if supportsApplyV4WithOverrides(style, nil) {
		t.Fatal("creative stages accepted without target masks")
	}
	if !supportsApplyV4WithOverrides(style, json.RawMessage(`{"mask_refs":[{"purpose":"inpaint_target"},{"purpose":"generation_target"}]}`)) {
		t.Fatal("creative stages rejected with explicit targets")
	}
}

func TestMergeOverrideNeedsDrivesIdentitySaveEditsAdmission(t *testing.T) {
	var style styleExecutionNeeds
	if err := json.Unmarshal(identityStyleJSON, &style); err != nil {
		t.Fatal(err)
	}
	override := json.RawMessage(`{"modules":{"beauty":{"enabled":true},"geometry":{"enabled":true},"extensions":{"restoration":{"denoise":"scunet_color_real_psnr","upscale":"2x"},"composition":{"enabled":true}}}}`)
	if err := mergeOverrideNeeds(&style, override); err != nil {
		t.Fatal(err)
	}
	if !style.Beauty.Enabled || !style.Geometry.Enabled || style.Extensions.Restoration.Denoise != denoiseModelID || style.Extensions.Restoration.Upscale != "2x" || !style.Extensions.Composition.Enabled {
		t.Fatalf("override stages lost: %+v", style.Extensions)
	}
}

func TestSupportsApplyV4RestorationAndRequiresPinnedModels(t *testing.T) {
	var style styleExecutionNeeds
	if err := json.Unmarshal([]byte(`{"mode":"fixed","extensions":{"background":{"mode":"keep"},"restoration":{"denoise":"scunet_color_real_psnr","denoise_strength":0.5,"upscale":"4x"}}}`), &style); err != nil {
		t.Fatal(err)
	}
	if !supportsApplyV4(style) {
		t.Fatal("materialized restoration plan rejected")
	}
	style.Models = []styleModelReference{
		{ID: denoiseModelID, WeightID: denoiseWeightID},
		{ID: upscale4ModelID, WeightID: upscale4WeightID},
	}
	if !hasExactModel(style.Models, denoiseModelID, denoiseWeightID) || !hasExactModel(style.Models, upscale4ModelID, upscale4WeightID) {
		t.Fatal("exact restoration model binding rejected")
	}
	style.Models[1].WeightID = "latest"
	if hasExactModel(style.Models, upscale4ModelID, upscale4WeightID) {
		t.Fatal("unpinned upscale model accepted")
	}
}

func TestSupportsApplyV4BackgroundCompose(t *testing.T) {
	for _, mode := range []string{"transparent", "solid", "blur"} {
		var style styleExecutionNeeds
		if err := json.Unmarshal([]byte(`{"mode":"fixed","extensions":{"background":{"mode":"`+mode+`"},"restoration":{"denoise":"off","upscale":"off"}}}`), &style); err != nil {
			t.Fatal(err)
		}
		if !supportsApplyV4(style) {
			t.Fatalf("background mode %s rejected", mode)
		}
	}
}

func TestBackgroundBlurRequiresPinnedDepthModel(t *testing.T) {
	models := []styleModelReference{{ID: depthModelID, WeightID: "latest"}}
	if hasExactModel(models, depthModelID, depthWeightID) {
		t.Fatal("unpinned depth model accepted")
	}
	models[0].WeightID = depthWeightID
	if !hasExactModel(models, depthModelID, depthWeightID) {
		t.Fatal("pinned depth model rejected")
	}
}

func TestApplyV4PortraitDetailRequiresExplicitOverrideRegionsAndTransformsGeometry(t *testing.T) {
	var style styleExecutionNeeds
	if err := json.Unmarshal([]byte(`{"mode":"fixed","extensions":{"portrait_detail":{"enabled":true},"background":{"mode":"keep"}}}`), &style); err != nil {
		t.Fatal(err)
	}
	if supportsApplyV4WithOverrides(style, nil) {
		t.Fatal("portrait detail without explicit regions accepted")
	}
	override := json.RawMessage(`{"mask_refs":[{"purpose":"portrait_teeth"}]}`)
	if !supportsApplyV4WithOverrides(style, override) {
		t.Fatal("explicit portrait detail regions rejected")
	}
	style.Geometry.Enabled = true
	if !supportsApplyV4WithOverrides(style, override) {
		t.Fatal("portrait regions with dedicated geometry transform rejected")
	}
}

func TestSupportsApplyV4ReviewedDefects(t *testing.T) {
	var style styleExecutionNeeds
	if err := json.Unmarshal([]byte(`{"mode":"fixed","defects":{"enabled":true,"policy":"review"},"extensions":{"background":{"mode":"keep"},"restoration":{"denoise":"off","upscale":"off"}}}`), &style); err != nil {
		t.Fatal(err)
	}
	if !supportsApplyV4(style) {
		t.Fatal("reviewed defects plan rejected")
	}
	style.Defects.Policy = ""
	if supportsApplyV4(style) {
		t.Fatal("defects without explicit review policy accepted")
	}
}

func TestSupportsApplyV4BeautyAndRequiresPinnedFaceModels(t *testing.T) {
	var style styleExecutionNeeds
	if err := json.Unmarshal([]byte(`{"mode":"fixed","beauty":{"enabled":true},"extensions":{"background":{"mode":"keep"},"restoration":{"denoise":"off","upscale":"off"}}}`), &style); err != nil {
		t.Fatal(err)
	}
	if !supportsApplyV4(style) {
		t.Fatal("materialized beauty plan rejected")
	}
	style.Models = []styleModelReference{
		{ID: faceDetectModelID, WeightID: faceDetectWeightID},
		{ID: faceLandmarkModelID, WeightID: faceLandmarkWeightID},
		{ID: faceParseModelID, WeightID: faceParseWeightID},
	}
	for _, model := range style.Models {
		if !hasExactModel(style.Models, model.ID, model.WeightID) {
			t.Fatalf("exact face model binding rejected: %s", model.ID)
		}
	}
	style.Models[2].WeightID = "latest"
	if hasExactModel(style.Models, faceParseModelID, faceParseWeightID) {
		t.Fatal("unpinned face parse model accepted")
	}
}

func TestSupportsApplyV4GeometryAndRequiresExactPoseModel(t *testing.T) {
	var style styleExecutionNeeds
	if err := json.Unmarshal([]byte(`{"mode":"fixed","geometry":{"enabled":true},"extensions":{"background":{"mode":"keep"},"restoration":{"denoise":"off","upscale":"off"}}}`), &style); err != nil {
		t.Fatal(err)
	}
	if !supportsApplyV4(style) {
		t.Fatal("materialized geometry plan rejected")
	}
	style.Models = []styleModelReference{{ID: poseModelID, WeightID: "latest"}}
	if hasExactModel(style.Models, poseModelID, poseWeightID) {
		t.Fatal("pose model with unpinned weights accepted")
	}
	style.Models[0].WeightID = poseWeightID
	if !hasExactModel(style.Models, poseModelID, poseWeightID) {
		t.Fatal("exact pose model binding rejected")
	}
}

func TestApplyNeedsJobThenItemLocalPrecedence(t *testing.T) {
	var needs styleExecutionNeeds
	if err := mergeOverrideNeeds(&needs, json.RawMessage(`{"modules":{"local":{"enabled":true},"geometry":{"enabled":true}}}`)); err != nil { t.Fatal(err) }
	if !needs.Local.Enabled || !needs.Geometry.Enabled { t.Fatal("job requirements lost") }
	if err := mergeOverrideNeeds(&needs, json.RawMessage(`{"modules":{"local":{"enabled":false}}}`)); err != nil { t.Fatal(err) }
	if needs.Local.Enabled || !needs.Geometry.Enabled { t.Fatal("item precedence lost inherited geometry") }
}

func TestApplyNeedsNullModulesKeepInheritedRequirements(t *testing.T) {
	var needs styleExecutionNeeds
	needs.Local.Enabled = true
	needs.Geometry.Enabled = true
	needs.Extensions.Restoration.Upscale = "2x"
	if err := mergeOverrideNeeds(&needs, json.RawMessage(`{"modules":{"local":null,"geometry":null,"extensions":null}}`)); err != nil { t.Fatal(err) }
	if !needs.Local.Enabled || !needs.Geometry.Enabled || needs.Extensions.Restoration.Upscale != "2x" { t.Fatal("null changed inherited requirements") }
}

func TestApplyNeedsNamedExtensionReplacesOldFields(t *testing.T) {
	var needs styleExecutionNeeds
	needs.Extensions.Restoration.Denoise = denoiseModelID
	needs.Extensions.Restoration.Upscale = "4x"
	needs.Extensions.Restoration.SensorDust = "review"
	needs.Extensions.Background.Mode = "blur"
	needs.Defects.Enabled = true
	needs.Defects.Policy = "review"
	if err := mergeOverrideNeeds(&needs, json.RawMessage(`{"modules":{"defects":{"enabled":false},"extensions":{"restoration":{"denoise":"off"}}}}`)); err != nil { t.Fatal(err) }
	if needs.Extensions.Restoration.Upscale != "" || needs.Extensions.Restoration.SensorDust != "" || needs.Extensions.Restoration.Denoise != "off" { t.Fatal("old restoration requirements survived replacement") }
	if needs.Defects.Enabled || needs.Defects.Policy != "" { t.Fatal("old defect policy survived replacement") }
	if needs.Extensions.Background.Mode != "blur" { t.Fatal("unnamed extension lost") }
}

func TestApplyGeneratedBackgroundRequiresEffectiveGeneration(t *testing.T) {
 var needs styleExecutionNeeds
 needs.Extensions.Background.Mode = "generate"
 needs.Extensions.Generation.Enabled = true
 if err := validateApplyModuleDependencies(needs); err != nil { t.Fatal(err) }
 if err := mergeOverrideNeeds(&needs,json.RawMessage(`{"modules":{"extensions":{"generation":{"enabled":false}}}}`)); err != nil { t.Fatal(err) }
 if err := validateApplyModuleDependencies(needs); err == nil { t.Fatal("generated background accepted after generation disabled") }
 if err := mergeOverrideNeeds(&needs,json.RawMessage(`{"modules":{"extensions":{"background":{"mode":"keep"}}}}`)); err != nil { t.Fatal(err) }
 if err := validateApplyModuleDependencies(needs); err != nil { t.Fatal(err) }
}

func TestApplyMaskingOverrideRequiresStyleModels(t *testing.T) {
 var needs styleExecutionNeeds
 if err:=mergeOverrideNeeds(&needs,json.RawMessage(`{"modules":{"extensions":{"masking":{"scene":"segformer_b0_ade","face":"bisenet_face19","interactive":"mobilesam"}}}}`));err!=nil{t.Fatal(err)}
 if applyMaskingModelsPresent(needs){t.Fatal("missing masking models accepted")}
 needs.Models=[]styleModelReference{{ID:"segformer_b0_ade",WeightID:"scene"},{ID:"bisenet_face19",WeightID:"face"},{ID:"mobilesam",WeightID:"interactive"}}
 if !applyMaskingModelsPresent(needs){t.Fatal("bound models rejected")}
 if err:=mergeOverrideNeeds(&needs,json.RawMessage(`{"modules":{"extensions":{"masking":null}}}`));err!=nil{t.Fatal(err)}
 needs.Models=nil
 if !applyMaskingModelsPresent(needs){t.Fatal("cleared masking still needs models")}
}

func TestApplyV4GenerationPreservesAdmittedStepRange(t *testing.T) {
	var style styleExecutionNeeds
	if err := json.Unmarshal([]byte(`{"mode":"fixed","extensions":{"generation":{"enabled":true,"model":"sd15_inpaint","prompt":"fill","steps":1,"guidance_scale":7.5,"require_acceptance":true}}}`), &style); err != nil { t.Fatal(err) }
	for _, steps := range []int{1, 12, 25, 50} {
		style.Extensions.Generation.Steps = steps
		if !supportsApplyV4WithOverrides(style, json.RawMessage(`{"mask_refs":[{"purpose":"generation_target"}]}`)) { t.Fatalf("valid generation steps %d fell back from v4", steps) }
	}
	for _, steps := range []int{0, 51} {
		style.Extensions.Generation.Steps = steps
		if supportsApplyV4WithOverrides(style, json.RawMessage(`{"mask_refs":[{"purpose":"generation_target"}]}`)) { t.Fatalf("invalid generation steps %d admitted", steps) }
	}
}

func TestApplyV4FullGenerationBoundsDoNotFallBack(t *testing.T) {
 var style styleExecutionNeeds
 if err:=json.Unmarshal([]byte(`{"mode":"fixed","extensions":{"generation":{"enabled":true,"model":"sd15_inpaint","prompt":"","negative_prompt":"","steps":50,"guidance_scale":0,"strength":0.25,"require_acceptance":true}}}`),&style);err!=nil { t.Fatal(err) }
 if !supportsApplyV4WithOverrides(style,json.RawMessage(`{"mask_refs":[{"purpose":"generation_target"}]}`)) { t.Fatal("valid zero guidance/empty prompt/partial strength fell back") }
 style.Extensions.Generation.Strength=1.1
 if supportsApplyV4WithOverrides(style,json.RawMessage(`{"mask_refs":[{"purpose":"generation_target"}]}`)) { t.Fatal("invalid strength admitted") }
}

func TestConservativePoliciesStayOnCompleteV4Graph(t *testing.T){
 var style styleExecutionNeeds
 style.Defects.Enabled=true;style.Defects.Policy="conservative_auto";style.Extensions.Restoration.SensorDust="conservative_auto"
 if !supportsApplyV4(style){t.Fatal("conservative repair must not fall back to graph3")}
 style.Defects.Policy="automatic";if supportsApplyV4(style){t.Fatal("unknown automatic policy admitted")}
}
