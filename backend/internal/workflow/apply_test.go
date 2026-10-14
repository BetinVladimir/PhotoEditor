package workflow

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

func TestApplyImageRefsDeclarePixelsAndAlpha(t *testing.T) {
	manifest := review.ArtifactRef{ID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	pixels := review.ArtifactRef{ID: uuid.New(), SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	alpha := review.ArtifactRef{ID: uuid.New(), SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	w := review.WorkingImage{Pixels: pixels, Alpha: &alpha}
	refs := refsForImage(manifest, w)
	if len(refs) != 3 || refs[0] != manifest || refs[1] != pixels || refs[2] != alpha {
		t.Fatalf("undeclared image storage refs: %#v", refs)
	}
	w.Alpha = nil
	if refs = refsForImage(manifest, w); len(refs) != 2 {
		t.Fatalf("unexpected alpha binding: %#v", refs)
	}
}

func TestApplyProfileAccountsFullFloatOutput(t *testing.T) {
	raw, err := applyProfile(12000000, 1048576)
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Backend  string           `json:"backend"`
		Estimate ResourceEstimate `json:"resource_estimate"`
	}
	if err = json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Backend != "cpu" || profile.Estimate.PixelCount != 12000000 || profile.Estimate.OutputBytes < 12000000*12 {
		t.Fatalf("unsafe profile: %#v", profile)
	}
}

func TestApplyRestorationAndBackgroundProfilesAccountTransformedOutputs(t *testing.T) {
	upscale, err := applyUpscaleProfile("cuda", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 100, 1024, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	transparent, err := applyBackgroundProfile(100, 1024, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]json.RawMessage{"upscale": upscale, "transparent": transparent} {
		var profile struct {
			Estimate ResourceEstimate `json:"resource_estimate"`
		}
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatal(err)
		}
		if name == "upscale" && (profile.Estimate.PixelCount != 1600 || profile.Estimate.OutputBytes < 1600*14) {
			t.Fatalf("unsafe upscale profile: %#v", profile)
		}
		if name == "transparent" && (profile.Estimate.PixelCount != 100 || profile.Estimate.OutputBytes < 100*14) {
			t.Fatalf("unsafe transparent profile: %#v", profile)
		}
	}
	if _, err := applyUpscaleProfile("cuda", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 200000000, 1024, 4, false); err == nil {
		t.Fatal("oversized upscale profile accepted")
	}
}

func TestApplyPersonMaskBindingPinsGeometryAndRuntime(t *testing.T) {
	lock := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	canvas := review.Canvas{Width: 32, Height: 24, Orientation: 1, SpaceID: "work", SourceSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	chain := json.RawMessage(`{"hash":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","steps":[]}`)
	image := review.WorkingImage{Canvas: canvas, TransformChain: chain}
	mask := review.MaskRef{Canvas: canvas, TransformChain: append(json.RawMessage(nil), chain...), Selector: "person", ModelLockID: &lock}
	if err := validateApplyPersonMask(mask, image, lock); err != nil {
		t.Fatal(err)
	}

	badLock := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	mask.ModelLockID = &badLock
	if err := validateApplyPersonMask(mask, image, lock); err == nil {
		t.Fatal("mask from another runtime lock accepted")
	}
	mask.ModelLockID = &lock
	mask.Canvas.Width++
	if err := validateApplyPersonMask(mask, image, lock); err == nil {
		t.Fatal("mask from another canvas accepted")
	}
}

func TestApplyQualityStatusIsClosedSet(t *testing.T) {
	for _, status := range []string{"pass", "review", "fail"} {
		got, err := parseApplyQualityStatus([]byte(`{"status":"` + status + `","technical":{}}`))
		if err != nil || got != status {
			t.Fatalf("status %q rejected: %q %v", status, got, err)
		}
	}
	for _, raw := range [][]byte{[]byte(`{}`), []byte(`{"status":"unknown"}`), []byte(`not-json`)} {
		if _, err := parseApplyQualityStatus(raw); err == nil {
			t.Fatalf("invalid quality report accepted: %s", raw)
		}
	}
}

func TestApplyCropProfileAccountsTransformedCanvasAndAlpha(t *testing.T) {
	for _, alpha := range []bool{false, true} {
		raw, err := applyCropProfile(12000000, 24000000, 4096, alpha)
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Estimate ResourceEstimate `json:"resource_estimate"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		bpp, inputRAM, outputRAM := int64(12), int64(64), int64(96)
		if alpha {
			bpp, inputRAM, outputRAM = 14, 72, 120
		}
		if p.Estimate.PixelCount != 24000000 || p.Estimate.RAMBytes < inputRAM*12000000+outputRAM*24000000+128*1048576 || p.Estimate.ScratchBytes < bpp*(12000000+24000000)+8192+65536 || p.Estimate.OutputBytes < bpp*24000000+2*1048576 {
			t.Fatalf("underbudgeted crop: %#v", p)
		}
	}
	for _, dimensions := range [][3]int64{{0, 1, 0}, {1, 0, 0}, {200000001, 1, 0}, {1, 200000001, 0}, {1, 1, -1}, {1, 1, 1048577}} {
		if _, err := applyCropProfile(dimensions[0], dimensions[1], dimensions[2], true); err == nil {
			t.Fatalf("invalid dimensions accepted: %v", dimensions)
		}
	}
}
