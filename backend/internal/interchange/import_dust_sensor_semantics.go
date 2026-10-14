package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

// The reader must return current, tenant-scoped, ingest-attested metadata for
// the exact target version/master. It may not infer a camera from image pixels.
type ImportDustSensorMetadataReader interface {
	ReadImportDustSensor(context.Context, v2input.SessionVersionBinding) (workflow.SensorAlignment, error)
}

type MetadataImportDustSeriesAuthority struct {
	Bindings []ImportCorrectionBinding
	Sensors  ImportDustSensorMetadataReader
}

type importDustSeriesHeader struct {
	Frames       []review.ArtifactRef `json:"frames"`
	CameraID     string               `json:"camera_id"`
	SensorWidth  int64                `json:"sensor_width"`
	SensorHeight int64                `json:"sensor_height"`
	RawCrop      struct {
		X      int64 `json:"x"`
		Y      int64 `json:"y"`
		Width  int64 `json:"width"`
		Height int64 `json:"height"`
	} `json:"raw_crop"`
}

func (a MetadataImportDustSeriesAuthority) binding(old review.ArtifactRef) (ImportCorrectionBinding, error) {
	var found *ImportCorrectionBinding
	for i := range a.Bindings {
		item := &a.Bindings[i]
		if item.Old.Source.ArtifactID == old.ID && item.Old.Source.SHA256 == old.SHA256 {
			if found != nil {
				return ImportCorrectionBinding{}, v2input.ErrInvalid
			}
			found = item
		}
	}
	if found == nil || !validImportRef(found.New.Source) {
		return ImportCorrectionBinding{}, v2input.ErrInvalid
	}
	return *found, nil
}

func (a MetadataImportDustSeriesAuthority) match(ctx context.Context, binding v2input.SessionVersionBinding, header importDustSeriesHeader) error {
	if a.Sensors == nil {
		return v2input.ErrInvalid
	}
	s, err := a.Sensors.ReadImportDustSensor(ctx, binding)
	if err != nil {
		return err
	}
	if s.CameraID != header.CameraID || s.SensorWidth != header.SensorWidth || s.SensorHeight != header.SensorHeight || s.CropX != header.RawCrop.X || s.CropY != header.RawCrop.Y || s.CropWidth != header.RawCrop.Width || s.CropHeight != header.RawCrop.Height || s.Orientation != 1 {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

func parseImportDustSeriesHeader(ctx context.Context, raw []byte, base review.ArtifactRef) (importDustSeriesHeader, error) {
	var header importDustSeriesHeader
	if _, err := review.ReadDustProposal(ctx, bytes.NewReader(raw), review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(raw)}, base); err != nil {
		return header, err
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return header, err
	}
	return header, nil
}

func (a MetadataImportDustSeriesAuthority) ValidateArchivedDustSeries(ctx context.Context, raw []byte, old v2input.SessionVersionBinding) error {
	if a.Sensors == nil {
		return v2input.ErrInvalid
	}
	header, err := parseImportDustSeriesHeader(ctx, raw, review.ArtifactRef{ID: old.Source.ArtifactID, SHA256: old.Source.SHA256})
	if err != nil {
		return err
	}
	for _, frame := range header.Frames {
		pair, err := a.binding(frame)
		if err != nil {
			return err
		}
		if err := a.match(ctx, pair.New, header); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (a MetadataImportDustSeriesAuthority) MapDustSeriesFrame(ctx context.Context, frame review.ArtifactRef, _, _ v2input.SessionVersionBinding) (review.ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return review.ArtifactRef{}, err
	}
	pair, err := a.binding(frame)
	if err != nil {
		return review.ArtifactRef{}, err
	}
	if a.Sensors == nil {
		return review.ArtifactRef{}, v2input.ErrInvalid
	}
	if _, err := a.Sensors.ReadImportDustSensor(ctx, pair.New); err != nil {
		return review.ArtifactRef{}, err
	}
	return review.ArtifactRef{ID: pair.New.Source.ArtifactID, SHA256: pair.New.Source.SHA256}, ctx.Err()
}

func (a MetadataImportDustSeriesAuthority) QualifyPreparedDustSeries(ctx context.Context, raw []byte, target v2input.SessionVersionBinding) error {
	header, err := parseImportDustSeriesHeader(ctx, raw, review.ArtifactRef{ID: target.Source.ArtifactID, SHA256: target.Source.SHA256})
	if err != nil {
		return err
	}
	for _, frame := range header.Frames {
		found := false
		for _, pair := range a.Bindings {
			if pair.New.Source.ArtifactID == frame.ID && pair.New.Source.SHA256 == frame.SHA256 {
				if found {
					return v2input.ErrInvalid
				}
				if err := a.match(ctx, pair.New, header); err != nil {
					return err
				}
				found = true
			}
		}
		if !found {
			return v2input.ErrInvalid
		}
	}
	return ctx.Err()
}

var _ ImportDustSeriesAuthority = MetadataImportDustSeriesAuthority{}
