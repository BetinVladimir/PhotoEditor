package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

type dustSensorReaderStub struct{ value workflow.SensorAlignment }

func (s dustSensorReaderStub) ReadImportDustSensor(context.Context, v2input.SessionVersionBinding) (workflow.SensorAlignment, error) {
	return s.value, nil
}

func TestDustSensorSemanticsRejectsCameraAndCropMismatch(t *testing.T) {
	ref := func(s string) v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(s))} }
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref("old"), CanvasHash: rawSHA([]byte("canvas"))}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref("target"), CanvasHash: old.CanvasHash}
	sensor := workflow.SensorAlignment{CameraID: "raw-" + rawSHA([]byte("camera")), SensorWidth: 420, SensorHeight: 320, CropX: 10, CropY: 10, CropWidth: 400, CropHeight: 300, Orientation: 1}
	header := importDustSeriesHeader{CameraID: sensor.CameraID, SensorWidth: sensor.SensorWidth, SensorHeight: sensor.SensorHeight}
	header.RawCrop.X, header.RawCrop.Y, header.RawCrop.Width, header.RawCrop.Height = sensor.CropX, sensor.CropY, sensor.CropWidth, sensor.CropHeight
	authority := MetadataImportDustSeriesAuthority{Bindings: []ImportCorrectionBinding{{Entry: uuid.New(), Old: old, New: target}}, Sensors: dustSensorReaderStub{value: sensor}}
	if err := authority.match(context.Background(), target, header); err != nil {
		t.Fatal("exact sensor alignment rejected", err)
	}
	header.RawCrop.Width++
	if err := authority.match(context.Background(), target, header); err == nil {
		t.Fatal("changed crop accepted")
	}
	header.RawCrop.Width--
	header.CameraID = "raw-" + rawSHA([]byte("other camera"))
	if err := authority.match(context.Background(), target, header); err == nil {
		t.Fatal("foreign camera accepted")
	}
	frame := review.ArtifactRef{ID: old.Source.ArtifactID, SHA256: old.Source.SHA256}
	mapped, err := authority.MapDustSeriesFrame(context.Background(), frame, old, target)
	if err != nil || mapped.ID != target.Source.ArtifactID || mapped.SHA256 != target.Source.SHA256 {
		t.Fatal("adopted frame mapping failed", err)
	}
}
