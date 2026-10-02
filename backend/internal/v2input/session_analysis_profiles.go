package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type SQLSessionDescribeProfiles struct {
	DB             *pgxpool.Pool
	Objects        SessionAnalysisObjectReader
	Bucket         string
	Backend        string
	RAMLimitBytes  int64
	VRAMLimitBytes int64
}

type sessionDescribeEstimate struct {
	RAMBytes     int64 `json:"ram_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
	PixelCount   int64 `json:"pixel_count"`
}

type sessionDescribeExecutionProfile struct {
	Backend          string                  `json:"backend"`
	RendererVersion  string                  `json:"renderer_version"`
	ResourceEstimate sessionDescribeEstimate `json:"resource_estimate"`
}

func compileSessionDescribeProfile(backend string, ramLimit, vramLimit int64,
	pixels []int64) (json.RawMessage, error) {
	if (backend != "cuda" && backend != "cpu" && backend != "mps") ||
		ramLimit < 2<<30 || len(pixels) < 1 || len(pixels) > 128 {
		return nil, ErrInvalid
	}
	vram := int64(0)
	if backend == "cuda" || backend == "mps" {
		vram = 6 << 30
		if vramLimit < vram {
			return nil, ErrInvalid
		}
	}
	var total, maximum int64
	for _, count := range pixels {
		if count < 1 || count > 200000000 {
			return nil, ErrInvalid
		}
		total += count
		if count > maximum {
			maximum = count
		}
	}
	estimate := sessionDescribeEstimate{
		RAMBytes:     2<<30 + 64*maximum,
		VRAMBytes:    vram,
		ScratchBytes: 1<<30 + 12*maximum,
		OutputBytes:  8<<20 + int64(len(pixels))*(512<<10),
		PixelCount:   total,
	}
	if estimate.RAMBytes > ramLimit {
		return nil, ErrInvalid
	}
	body, err := canonical.Marshal(sessionDescribeExecutionProfile{
		backend, "v2.2", estimate})
	if err != nil || !validSessionDescribeProfile(body) {
		return nil, ErrInvalid
	}
	return body, nil
}

// Compile derives per-shard memory from verified frozen WorkingImage canvas
// dimensions. Shards process images sequentially, so RAM tracks the largest
// frame while pixel_count records the complete workload for scheduling.
func (s SQLSessionDescribeProfiles) Compile(ctx context.Context,
	tenant, run, workflow uuid.UUID,
	snapshot SessionInputSnapshot) ([]json.RawMessage, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil ||
		run == uuid.Nil || workflow == uuid.Nil ||
		(s.Backend != "cuda" && s.Backend != "cpu" && s.Backend != "mps") ||
		s.RAMLimitBytes < 2<<30 || len(snapshot.Items) < 1 ||
		len(snapshot.Items) > 5000 {
		return nil, ErrInvalid
	}
	if _, err := compileSessionDescribeProfile(s.Backend,
		s.RAMLimitBytes, s.VRAMLimitBytes, []int64{1}); err != nil {
		return nil, err
	}
	plan, err := CompileSessionDescriptors(workflow, len(snapshot.Items))
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT ordinal,asset_version_id,artifact_id,sha256,
	 bytes,bucket,object_key,object_version,media_type,captured_at,camera_hash
	 FROM photo.v22_analysis_image_sources($1,$2)`, tenant, run)
	if err != nil {
		return nil, err
	}
	type source struct {
		Object objectstore.Object
		Ref    Ref
	}
	sources := make([]source, 0, len(snapshot.Items))
	for rows.Next() {
		var ordinal int
		var version uuid.UUID
		var bucket string
		var capturedAt, cameraHash *string
		var row source
		if err := rows.Scan(&ordinal, &version, &row.Ref.ArtifactID,
			&row.Ref.SHA256, &row.Object.Bytes, &bucket,
			&row.Object.Key, &row.Object.VersionID,
			&row.Object.ContentType, &capturedAt, &cameraHash); err != nil {
			rows.Close()
			return nil, err
		}
		if ordinal != len(sources) || ordinal >= len(snapshot.Items) ||
			version != snapshot.Items[ordinal].Binding.AssetVersionID ||
			row.Ref != snapshot.Items[ordinal].WorkingImage ||
			bucket != s.Bucket ||
			!equalOptionalString(capturedAt, snapshot.Items[ordinal].CapturedAt) ||
			!equalOptionalString(cameraHash, snapshot.Items[ordinal].CameraHash) {
			rows.Close()
			return nil, ErrInvalid
		}
		row.Object.SHA256 = row.Ref.SHA256
		sources = append(sources, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(sources) != len(snapshot.Items) {
		return nil, ErrInvalid
	}
	pixels := make([]int64, len(sources))
	for i, row := range sources {
		image, err := readSessionWorkingImage(ctx, s.Objects,
			row.Object, row.Ref, snapshot.Items[i].Binding.CanvasHash)
		if err != nil {
			return nil, err
		}
		count := image.Canvas.Width * image.Canvas.Height
		if count < 1 || count > 200000000 {
			return nil, ErrInvalid
		}
		pixels[i] = count
	}
	profiles := make([]json.RawMessage, len(plan.Tasks))
	for shard, task := range plan.Tasks {
		shardPixels := make([]int64, 0, len(task.Ordinals))
		for _, ordinal := range task.Ordinals {
			shardPixels = append(shardPixels, pixels[ordinal])
		}
		body, err := compileSessionDescribeProfile(s.Backend,
			s.RAMLimitBytes, s.VRAMLimitBytes, shardPixels)
		if err != nil {
			return nil, err
		}
		profiles[shard] = body
	}
	return profiles, nil
}
