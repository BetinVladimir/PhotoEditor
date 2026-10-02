package workflow

import (
	"encoding/json"
	"fmt"
	"testing"

	"photoeditor/backend/internal/review"
)

func dustItem(ordinal int, camera string) *applyInput {
	s := &SensorAlignment{CameraID: camera, SensorWidth: 420, SensorHeight: 320, CropX: 10, CropY: 10, CropWidth: 400, CropHeight: 300, Orientation: 1}
	return &applyInput{Ordinal: ordinal, Sensor: s, Working: review.WorkingImage{Canvas: review.Canvas{Width: 400, Height: 300, Orientation: 1}}}
}

func TestCompileApplyDustGroupsIsStableAndNeverLeavesShortTail(t *testing.T) {
	items := make([]applyInput, 0, 69)
	for i := 64; i >= 0; i-- {
		items = append(items, *dustItem(i, "raw-"+fmt.Sprintf("%064x", 1)))
	}
	for i := 0; i < 4; i++ {
		items = append(items, *dustItem(100+i, "raw-"+fmt.Sprintf("%064x", 2)))
	}
	groups, err := compileApplyDustGroups(items)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || len(groups[0].Items) != 33 || len(groups[1].Items) != 32 {
		t.Fatalf("groups=%v/%v", len(groups), func() []int {
			v := []int{}
			for _, g := range groups {
				v = append(v, len(g.Items))
			}
			return v
		}())
	}
	if groups[0].Items[0].Ordinal != 0 || groups[1].Items[len(groups[1].Items)-1].Ordinal != 64 {
		t.Fatal("ordinal order lost")
	}
}

func TestParseApplySensorAlignmentRejectsRasterAndGeometryMismatch(t *testing.T) {
	image := review.WorkingImage{Canvas: review.Canvas{Width: 400, Height: 300}}
	doc := map[string]any{"ingest": map[string]any{"format": "raw", "sensor_alignment": map[string]any{"camera_id": "raw-" + fmt.Sprintf("%064x", 1), "sensor_width": 420, "sensor_height": 320, "crop_x": 10, "crop_y": 10, "crop_width": 400, "crop_height": 300, "orientation": 1}}}
	raw, _ := json.Marshal(doc)
	got, err := parseApplySensorAlignment(raw, image)
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
	doc["ingest"].(map[string]any)["format"] = "jpeg"
	raw, _ = json.Marshal(doc)
	if _, err = parseApplySensorAlignment(raw, image); err == nil {
		t.Fatal("raster mapping accepted")
	}
}
