package v2region

import "encoding/json"

// SourceResourceBudget is a lower bound, excluding complete operator/model and
// presentation costs. Alpha reserves legacy uint16 normalization conservatively.
type SourceResourceBudget struct{ PixelCount, RAMBytes, ScratchBytes int64 }

func (s RegionSourceShape) ResourceBudget() (SourceResourceBudget, error) {
	if s.Width < 1 || s.Height < 1 || s.Width > 200000000 || s.Height > 200000000/s.Width {
		return SourceResourceBudget{}, ErrRegionInvalid
	}
	pixels := s.Width * s.Height
	inputs := 12*pixels + 4096
	normalization := int64(0)
	if s.HasAlpha {
		inputs += 4*pixels + 4096
		normalization = 4*pixels + 4096
	}
	rows := min(s.Height, int64(512)) * s.Width
	tile := min(s.Width, int64(512)) * min(s.Height, int64(512))
	return SourceResourceBudget{pixels, inputs + normalization + 12*rows + 16*tile + 16*1024*1024, inputs + normalization}, nil
}

// validateRegionSourceBudget checks only the source component of qualified policy.
func validateRegionSourceBudget(profile []byte, shape RegionSourceShape) error {
	required, err := shape.ResourceBudget()
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(profile, &fields) != nil {
		return ErrRegionInvalid
	}
	var estimate map[string]json.RawMessage
	if json.Unmarshal(fields["resource_estimate"], &estimate) != nil || estimate == nil {
		return ErrRegionInvalid
	}
	values := map[string]int64{}
	for _, key := range []string{"pixel_count", "ram_bytes", "scratch_bytes"} {
		var value int64
		if json.Unmarshal(estimate[key], &value) != nil || value < 0 || value > 9007199254740991 {
			return ErrRegionInvalid
		}
		values[key] = value
	}
	if values["pixel_count"] != required.PixelCount || values["ram_bytes"] < required.RAMBytes || values["scratch_bytes"] < required.ScratchBytes {
		return ErrRegionInvalid
	}
	return nil
}

// StyleV2 loading uses the existing worker TaskArtifacts.style parser, which
// requires 3GiB admitted RAM. TaskArtifacts.create also needs an output budget.
func validateRegionArtifactBudget(profile []byte) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(profile, &fields) != nil {
		return ErrRegionInvalid
	}
	var estimate map[string]json.RawMessage
	if json.Unmarshal(fields["resource_estimate"], &estimate) != nil {
		return ErrRegionInvalid
	}
	var ram, output int64
	if json.Unmarshal(estimate["ram_bytes"], &ram) != nil || ram < 3*1024*1024*1024 || ram > 9007199254740991 || json.Unmarshal(estimate["output_bytes"], &output) != nil || output < 1 || output > 9007199254740991 {
		return ErrRegionInvalid
	}
	return nil
}
