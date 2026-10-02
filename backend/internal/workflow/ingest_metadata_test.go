package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func metadataRef(raw []byte) review.ArtifactRef {
	h := sha256.Sum256(raw)
	return review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(h[:])}
}
func TestIngestMetadataContentAndSourceBinding(t *testing.T) {
	source := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	raw, _ := canonical.Marshal(inspectedAsset{Source: source, Width: 400, Height: 300, BitDepth: 16, Orientation: 6, HasAlpha: true, Format: "png"})
	ref := metadataRef(raw)
	got, err := ReadIngestMetadata(context.Background(), bytes.NewReader(raw), ref, source)
	if err != nil || got.Width*got.Height != 120000 || !got.HasAlpha {
		t.Fatal(got, err)
	}
	cases := map[string]func(map[string]any){"missing icc": func(d map[string]any) { delete(d, "icc_sha256") }, "null alpha": func(d map[string]any) { d["has_alpha"] = nil }, "huge pixels": func(d map[string]any) { d["width"] = 200000000 }, "unknown format": func(d map[string]any) { d["format"] = "svg" }, "bad orientation": func(d map[string]any) { d["orientation"] = 0 }, "bad depth": func(d map[string]any) { d["bit_depth"] = 32 }, "unknown field": func(d map[string]any) { d["extra"] = true }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var d map[string]any
			json.Unmarshal(raw, &d)
			change(d)
			changed, _ := canonical.Marshal(d)
			if _, err := ReadIngestMetadata(context.Background(), bytes.NewReader(changed), metadataRef(changed), source); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	wrong := source
	wrong.ID = uuid.New()
	if _, err = ReadIngestMetadata(context.Background(), bytes.NewReader(raw), ref, wrong); err == nil {
		t.Fatal("wrong source accepted")
	}
	if _, err = ReadIngestMetadata(context.Background(), bytes.NewReader(append(raw, ' ')), ref, source); err == nil {
		t.Fatal("wrong bytes accepted")
	}
	dup := append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"width":400}`)...)
	if _, err = ReadIngestMetadata(context.Background(), bytes.NewReader(dup), metadataRef(dup), source); err == nil {
		t.Fatal("duplicate accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = ReadIngestMetadata(ctx, bytes.NewReader(raw), ref, source); err != context.Canceled {
		t.Fatal("cancellation lost")
	}
}

func TestIngestMetadataAcceptsOnlyExactRawSensorAlignment(t *testing.T) {
	source := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	doc := map[string]any{"source": source, "width": 400, "height": 300, "bit_depth": 16, "has_alpha": false, "orientation": 1, "icc_sha256": nil, "format": "raw", "sensor_alignment": map[string]any{"camera_id": "raw-" + strings.Repeat("c", 64), "sensor_width": 420, "sensor_height": 320, "crop_x": 10, "crop_y": 10, "crop_width": 400, "crop_height": 300, "orientation": 1}}
	raw, _ := canonical.Marshal(doc)
	got, err := ReadIngestMetadata(context.Background(), bytes.NewReader(raw), metadataRef(raw), source)
	if err != nil || got.Sensor == nil || got.Sensor.CameraID != doc["sensor_alignment"].(map[string]any)["camera_id"] {
		t.Fatal(got, err)
	}
	doc["format"] = "jpeg"
	bad, _ := canonical.Marshal(doc)
	if _, err = ReadIngestMetadata(context.Background(), bytes.NewReader(bad), metadataRef(bad), source); err == nil {
		t.Fatal("raster sensor mapping accepted")
	}
}
