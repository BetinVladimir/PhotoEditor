package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strconv"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

// IngestMetadata comes from a verified canonical asset.inspect output. RAW
// visible dimensions do not bound sensor/intermediate memory, so RAW RAM uses
// the full supported pixel limit until measured qualification supplies a cap.
type IngestMetadata struct {
	Source      review.ArtifactRef `json:"source"`
	Width       int64              `json:"width"`
	Height      int64              `json:"height"`
	Orientation int                `json:"orientation"`
	HasAlpha    bool               `json:"has_alpha"`
	Format      string             `json:"format"`
	CapturedAt *string `json:"captured_at,omitempty"`
	CapturedLocal *string `json:"captured_local,omitempty"`
	Sensor      *SensorAlignment   `json:"sensor_alignment"`
}

type SensorAlignment struct {
	CameraID     string `json:"camera_id"`
	SensorWidth  int64  `json:"sensor_width"`
	SensorHeight int64  `json:"sensor_height"`
	CropX        int64  `json:"crop_x"`
	CropY        int64  `json:"crop_y"`
	CropWidth    int64  `json:"crop_width"`
	CropHeight   int64  `json:"crop_height"`
	Orientation  int    `json:"orientation"`
}

func PrepareIngestTask(node CompiledIngestNode, params review.ArtifactRef, sourceBytes int64, metadata *IngestMetadata) (PreparedTask, error) {
	fail := func() (PreparedTask, error) { return PreparedTask{}, errors.New("invalid ingest task binding") }
	if node.ID == uuid.Nil || !validExpansionRef(params) || len(node.Params) == 0 || len(node.Params) > 1048576 || len(node.Inputs) != 1 || !validExpansionRef(node.Inputs[0]) || params.ID == node.Inputs[0].ID {
		return fail()
	}
	hash := sha256.Sum256(node.Params)
	if hex.EncodeToString(hash[:]) != params.SHA256 {
		return fail()
	}
	var source ingestSourceParams
	var decode ingestDecodeParams
	if node.Operation == "asset.decode" {
		if json.Unmarshal(node.Params, &decode) != nil {
			return fail()
		}
		source = decode.ingestSourceParams
	} else {
		if json.Unmarshal(node.Params, &source) != nil {
			return fail()
		}
	}
	// Recompile this single item to validate all public plan fields and parameters.

	// Node keys have a fixed stage prefix followed by the snapshot ordinal.
	prefix := node.Operation + "/"
	if len(node.Key) <= len(prefix) || node.Key[:len(prefix)] != prefix {
		return fail()
	}
	ordinal, err := strconv.ParseInt(node.Key[len(prefix):], 10, 64)
	if err != nil {
		return fail()
	}
	registry, err := LoadRegistry()
	if err != nil {
		return PreparedTask{}, err
	}
	check, err := CompileIngest(IngestSpec{RunID: node.ID, Items: []IngestItem{{Ordinal: ordinal, Source: source.Source, Bytes: source.Bytes, Suffix: source.Suffix, AssumeProfile: decode.AssumeProfile, Analyze: decode.Analyze, Tile: decode.Tile, RAWDevelopment: decode.RAWDevelopment}}}, registry)
	if err != nil {
		return fail()
	}
	matched := false
	for _, compiled := range check.Nodes {
		if compiled.Key == node.Key && compiled.Operation == node.Operation && bytes.Equal(compiled.Params, node.Params) {
			matched = true
		}
	}
	if !matched {
		return fail()
	}
	op, exists := registry[node.Operation]
	if !exists || (node.Operation != "asset.verify" && node.Operation != "asset.inspect" && node.Operation != "asset.decode") || node.Version != op.Version || node.ResourceClass != op.ResourceClass || node.TimeoutSeconds != op.TimeoutSeconds || source.Source != node.Inputs[0] || source.Bytes != sourceBytes || sourceBytes < 1 || sourceBytes > 536870912 {
		return fail()
	}
	if metadata != nil && (metadata.Source != source.Source || metadata.Width < 1 || metadata.Height < 1 || metadata.Width > 200000000 || metadata.Height > 200000000 || metadata.Width*metadata.Height > 200000000) {
		return fail()
	}
	pixels := int64(200000000)
	alpha := false
	raw := source.Suffix != ".jpg" && source.Suffix != ".jpeg" && source.Suffix != ".png" && source.Suffix != ".tif" && source.Suffix != ".tiff"
	if metadata != nil {
		pixels = metadata.Width * metadata.Height
		alpha = metadata.HasAlpha
		switch metadata.Format {
		case "jpeg", "png", "tiff":
			if raw {
				return fail()
			}
		case "raw":
			if !raw || alpha {
				return fail()
			}
		default:
			return fail()
		}
	}
	const mib = int64(1048576)
	tile := 512
	output := mib
	if node.Operation == "asset.decode" {
		if metadata == nil || decode.Analyze && alpha {
			return fail()
		}
		tile = decode.Tile
		if tile != 256 && tile != 512 && tile != 1024 {
			return fail()
		}
		output = pixels*12 + 2*mib
		if alpha {
			output += pixels * 2
		}
	}
	ram := 1024*mib + int64(tile)*int64(tile)*128
	if raw {
		ram = 32*200000000 + 1024*mib
	}
	estimate := ResourceEstimate{RAMBytes: ram, ScratchBytes: sourceBytes + int64(len(node.Params)) + output, OutputBytes: output, PixelCount: pixels}
	profile, err := canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		MaxPixels int64            `json:"max_pixels"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{"cpu", "float32", tile, 200000000, estimate})
	if err != nil {
		return fail()
	}
	if _, err = ParseResourceEstimate(profile); err != nil {
		return fail()
	}
	digest, err := repairInputDigest(node.Operation, node.Version, params, node.Inputs)
	if err != nil {
		return fail()
	}
	return PreparedTask{ID: node.ID, NodeKey: node.Key, Operation: node.Operation, Version: node.Version, ResourceClass: node.ResourceClass, TimeoutSeconds: node.TimeoutSeconds, Params: params, Inputs: append([]review.ArtifactRef(nil), node.Inputs...), InputDigest: digest, ExecutionProfile: profile}, nil
}
