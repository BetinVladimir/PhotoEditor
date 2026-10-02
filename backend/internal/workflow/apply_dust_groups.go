package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"photoeditor/backend/internal/review"
)

type applyDustGroup struct {
	Key   string
	Items []applyInput
}

func parseApplySensorAlignment(raw json.RawMessage, image review.WorkingImage) (*SensorAlignment, error) {
	var envelope struct {
		Ingest struct {
			Format string           `json:"format"`
			Sensor *SensorAlignment `json:"sensor_alignment"`
		} `json:"ingest"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &envelope) != nil {
		return nil, errors.New("invalid apply asset metadata")
	}
	s := envelope.Ingest.Sensor
	if s == nil {
		return nil, nil
	}
	if envelope.Ingest.Format != "raw" || s.Orientation != 1 || s.CameraID == "" || s.CropWidth != int64(image.Canvas.Width) || s.CropHeight != int64(image.Canvas.Height) || s.CropX < 0 || s.CropY < 0 || s.SensorWidth < 8 || s.SensorHeight < 8 || s.CropX+s.CropWidth > s.SensorWidth || s.CropY+s.CropHeight > s.SensorHeight || image.Alpha != nil {
		return nil, errors.New("invalid apply sensor alignment")
	}
	return s, nil
}

func compileApplyDustGroups(items []applyInput) ([]applyDustGroup, error) {
	byKey := map[string][]applyInput{}
	for _, item := range items {
		if item.Sensor == nil {
			continue
		}
		s := item.Sensor
		key := fmt.Sprintf("%s/%d/%d/%d/%d/%d/%d/%d", s.CameraID, s.SensorWidth, s.SensorHeight, s.CropX, s.CropY, s.CropWidth, s.CropHeight, s.Orientation)
		byKey[key] = append(byKey[key], item)
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	groups := []applyDustGroup{}
	for _, key := range keys {
		members := byKey[key]
		sort.Slice(members, func(i, j int) bool { return members[i].Ordinal < members[j].Ordinal })
		if len(members) < 5 {
			continue
		}
		count := (len(members) + 63) / 64
		base, extra := len(members)/count, len(members)%count
		offset := 0
		for i := 0; i < count; i++ {
			size := base
			if i < extra {
				size++
			}
			if size < 5 || size > 64 {
				return nil, errors.New("invalid dust group partition")
			}
			groups = append(groups, applyDustGroup{Key: fmt.Sprintf("%s/%04d", key, i), Items: append([]applyInput(nil), members[offset:offset+size]...)})
			offset += size
		}
	}
	return groups, nil
}
