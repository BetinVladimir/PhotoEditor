package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

type inspectedAsset struct {
	Source      review.ArtifactRef `json:"source"`
	Width       int64              `json:"width"`
	Height      int64              `json:"height"`
	BitDepth    int                `json:"bit_depth"`
	HasAlpha    bool               `json:"has_alpha"`
	Orientation int                `json:"orientation"`
	ICCSHA256   *string            `json:"icc_sha256"`
	Format      string             `json:"format"`
	CapturedAt *string `json:"captured_at,omitempty"`
	CapturedLocal *string `json:"captured_local,omitempty"`
	Sensor      *struct {
		CameraID     string `json:"camera_id"`
		SensorWidth  int64  `json:"sensor_width"`
		SensorHeight int64  `json:"sensor_height"`
		CropX        int64  `json:"crop_x"`
		CropY        int64  `json:"crop_y"`
		CropWidth    int64  `json:"crop_width"`
		CropHeight   int64  `json:"crop_height"`
		Orientation  int    `json:"orientation"`
	} `json:"sensor_alignment"`
}

// ReadIngestMetadata verifies exact stored bytes and the complete worker wire
// contract before deriving resource inputs. Caller attests canonical asset.inspect
// producer, tenant ownership and immutable object version; reader remains owned
// by caller and must honor ctx when backed by network IO.
func ReadIngestMetadata(ctx context.Context, reader io.Reader, ref, source review.ArtifactRef) (IngestMetadata, error) {
	fail := func() (IngestMetadata, error) { return IngestMetadata{}, errors.New("invalid ingest metadata") }
	if reader == nil || !validExpansionRef(ref) || !validExpansionRef(source) {
		return fail()
	}
	if err := ctx.Err(); err != nil {
		return IngestMetadata{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 1048577))
	if err != nil {
		return IngestMetadata{}, err
	}
	if err = ctx.Err(); err != nil {
		return IngestMetadata{}, err
	}
	if len(raw) > 1048576 {
		return fail()
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return fail()
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return fail()
	}
	var d inspectedAsset
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil {
		return fail()
	}
	roundtrip, err := canonical.Marshal(d)
	if err != nil || !bytes.Equal(roundtrip, normalized) {
		return fail()
	}
	if d.Source != source || d.Width < 1 || d.Height < 1 || d.Width > 200000000 || d.Height > 200000000 || d.Width*d.Height > 200000000 || (d.BitDepth != 8 && d.BitDepth != 16) || d.Orientation < 1 || d.Orientation > 8 {
		return fail()
	}
	if d.CapturedAt!=nil&&d.CapturedLocal!=nil{return fail()}
	if d.CapturedAt!=nil{parsed,e:=time.Parse("2006-01-02T15:04:05Z",*d.CapturedAt);if e!=nil||parsed.Year()<1||parsed.Format("2006-01-02T15:04:05Z")!=*d.CapturedAt{return fail()}}
	if d.CapturedLocal!=nil{parsed,e:=time.Parse("2006-01-02T15:04:05",*d.CapturedLocal);if e!=nil||parsed.Year()<1||parsed.Format("2006-01-02T15:04:05")!=*d.CapturedLocal{return fail()}}
	if d.ICCSHA256 != nil {
		if _, err = domain.ParseDigest(*d.ICCSHA256); err != nil {
			return fail()
		}
	}
	switch d.Format {
	case "jpeg", "png", "tiff", "raw":
	default:
		return fail()
	}
	var sensor *SensorAlignment
	if d.Sensor != nil {
		if d.Format != "raw" || len(d.Sensor.CameraID) != 68 || d.Sensor.CameraID[:4] != "raw-" || d.Sensor.SensorWidth < 8 || d.Sensor.SensorHeight < 8 || d.Sensor.CropWidth < 8 || d.Sensor.CropHeight < 8 || d.Sensor.CropX < 0 || d.Sensor.CropY < 0 || d.Sensor.CropX+d.Sensor.CropWidth > d.Sensor.SensorWidth || d.Sensor.CropY+d.Sensor.CropHeight > d.Sensor.SensorHeight || d.Sensor.Orientation != 1 || d.Sensor.CropWidth != d.Width || d.Sensor.CropHeight != d.Height {
			return fail()
		}
		if _, err = domain.ParseDigest(d.Sensor.CameraID[4:]); err != nil {
			return fail()
		}
		sensor = &SensorAlignment{CameraID: d.Sensor.CameraID, SensorWidth: d.Sensor.SensorWidth, SensorHeight: d.Sensor.SensorHeight, CropX: d.Sensor.CropX, CropY: d.Sensor.CropY, CropWidth: d.Sensor.CropWidth, CropHeight: d.Sensor.CropHeight, Orientation: 1}
	}
	return IngestMetadata{Source: d.Source, Width: d.Width, Height: d.Height, Orientation: d.Orientation, HasAlpha: d.HasAlpha, Format: d.Format, Sensor: sensor,CapturedAt:d.CapturedAt,CapturedLocal:d.CapturedLocal}, nil
}
