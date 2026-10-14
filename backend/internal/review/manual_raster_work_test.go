package review

import (
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestManualRasterStreamWorkBound(t *testing.T) {
	radius, hardness := .1, .5
	canvas := Canvas{Width: 1025, Height: 2, Orientation: 1, SpaceID: "oriented:test", SourceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	vectors := manualVectors{Version: "manual_mask_v1", SourceSHA: canvas.SourceSHA256, Canvas: canvas, Shapes: []manualShape{
		{Kind: "stroke", Mode: "add", Sequence: 0, Points: []manualPoint{{X: .5, Y: .5}}, Radius: &radius, Hardness: &hardness},
		{Kind: "polygon", Mode: "protect", Sequence: 1, Points: []manualPoint{{}, {X: 1}, {Y: 1}}},
	}}
	body, _ := canonical.Marshal(vectors)
	plan := ManualRasterPlan{body: body, canvas: canvas, vectors: vectors}
	work, err := plan.EstimateStreamWork()
	if err != nil || work.Pixels != 2050 || work.RowSegments != 4 || work.ShapeBoundPointChecks != 16 || work.SampleEdgeEvaluations != 2050*13 || work.NPYPayloadBytes != 16400 || work.ExpectedSegmentBytes != 8192 {
		t.Fatal(work, err)
	}
	if _, err = plan.CheckStreamBudget(16, 26650, 16400); err != nil {
		t.Fatal(err)
	}
	for _, limit := range [][3]int64{{15, 26650, 16400}, {16, 26649, 16400}, {16, 26650, 16399}, {0, 0, 0}, {-1, 26650, 16400}} {
		if denied, err := plan.CheckStreamBudget(limit[0], limit[1], limit[2]); err == nil || denied != (ManualRasterWork{}) {
			t.Fatal("insufficient budget accepted")
		}
	}
	maximum := plan
	maximum.canvas.Width = 200000000
	maximum.canvas.Height = 1
	maximumWork, err := maximum.EstimateStreamWork()
	if err != nil || maximumWork.NPYPayloadBytes != 1600000000 || maximumWork.SampleEdgeEvaluations != 2600000000 || maximumWork.RowSegments != 195313 {
		t.Fatal("work overflow or truncation", maximumWork, err)
	}
	if _, err = (ManualRasterPlan{}).EstimateStreamWork(); err == nil {
		t.Fatal("zero work plan accepted")
	}
	if err = (ManualRasterWork{}).checkBudget(0, 0, 0); err == nil {
		t.Fatal("zero estimate accepted")
	}
}
