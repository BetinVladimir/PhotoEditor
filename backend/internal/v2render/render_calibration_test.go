package v2render

import (
	"photoeditor/backend/internal/domain"
	"strings"
	"testing"
)

func testRenderCalibration() RenderCalibration {
	return RenderCalibration{Qualification: RenderQualification{ModelSHA: strings.Repeat("a", 64), RuntimeSHA: strings.Repeat("b", 64), Backend: "cuda", DeviceSHA: strings.Repeat("c", 64), DatasetSHA: strings.Repeat("d", 64)}, EvidenceSHA: strings.Repeat("e", 64), ICCSHA: strings.Repeat("f", 64), TileSize: 512, AlphaMode: "require_float32", Samples: []RenderCalibrationSample{
		{Width: 400, Height: 400, Provider: RenderProviderPeak{RAMBytes: 100, ScratchBytes: 101, VRAMBytes: 300, OutputBytes: 203}},
		{Width: 800, Height: 200, Provider: RenderProviderPeak{RAMBytes: 200, ScratchBytes: 200, VRAMBytes: 600, OutputBytes: 400}},
	}}
}

func TestRenderCalibrationMeasuredBoundsAndRounding(t *testing.T) {
	c := testRenderCalibration()
	selected, err := c.Select(c.Qualification, 100, 200, c.ICCSHA)
	if err != nil || selected.Provider != (RenderProviderPeak{RAMBytes: 100, ScratchBytes: 16, VRAMBytes: 300, OutputBytes: 31}) {
		t.Fatal(selected, err)
	}
	c.Samples[0], c.Samples[1] = c.Samples[1], c.Samples[0]
	replay, err := c.Select(c.Qualification, 100, 200, c.ICCSHA)
	if err != nil || replay != selected {
		t.Fatal("registry order changed selection", replay, err)
	}
	portrait, err := c.Select(c.Qualification, 400, 300, c.ICCSHA)
	if err != nil || portrait.Provider.VRAMBytes != 300 {
		t.Fatal("area alone admitted wrong aspect", portrait, err)
	}
	for _, size := range [][2]int64{{801, 200}, {200, 401}} {
		if _, err := c.Select(c.Qualification, size[0], size[1], c.ICCSHA); err == nil || domain.AsError(err).Code != "RENDER_UNAVAILABLE" {
			t.Fatal("extrapolated measured dimensions", size, err)
		}
	}
}

func TestRenderCalibrationRejectsUnpinnedAndUnsafeRegistry(t *testing.T) {
	for _, change := range []func(*RenderCalibration){
		func(c *RenderCalibration) { c.Qualification.DeviceSHA = strings.Repeat("0", 64) },
		func(c *RenderCalibration) { c.EvidenceSHA = "unverified" },
		func(c *RenderCalibration) { c.Samples = nil },
		func(c *RenderCalibration) { c.Samples = append(c.Samples, c.Samples[0]) },
		func(c *RenderCalibration) { c.Samples[0].Provider.ScratchBytes = -1 },
		func(c *RenderCalibration) { c.Samples[0].Width = 200000001 },
		func(c *RenderCalibration) { c.Samples[0].Provider.OutputBytes = 9007199254000000 },
	} {
		c := testRenderCalibration()
		q := c.Qualification
		change(&c)
		if _, err := c.Select(q, 100, 200, c.ICCSHA); err == nil {
			t.Fatal("invalid calibration admitted", c)
		}
	}
	c := testRenderCalibration()
	if _, err := c.Select(c.Qualification, 100, 200, strings.Repeat("0", 64)); err == nil {
		t.Fatal("ICC mismatch accepted")
	}
	// A legal measured overhead can overflow after the mandated safety margin.
	c.Samples = []RenderCalibrationSample{{Width: 1, Height: 1, Provider: RenderProviderPeak{ScratchBytes: 8000000000000000}}}
	if _, err := c.Select(c.Qualification, 1, 1, c.ICCSHA); err == nil {
		t.Fatal("scaled safe-integer overflow accepted")
	}
}
