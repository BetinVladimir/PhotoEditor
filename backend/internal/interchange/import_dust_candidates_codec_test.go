package interchange

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type dustSeriesAuthorityStub struct {
	mapped map[review.ArtifactRef]review.ArtifactRef
}

func (dustSeriesAuthorityStub) ValidateArchivedDustSeries(context.Context, []byte, v2input.SessionVersionBinding) error {
	return nil
}
func (s dustSeriesAuthorityStub) MapDustSeriesFrame(_ context.Context, old review.ArtifactRef, _, _ v2input.SessionVersionBinding) (review.ArtifactRef, error) {
	return s.mapped[old], nil
}
func (dustSeriesAuthorityStub) QualifyPreparedDustSeries(context.Context, []byte, v2input.SessionVersionBinding) error {
	return nil
}

func TestImportDustCandidatesReissuesWholeSeries(t *testing.T) {
	frames := make([]review.ArtifactRef, 5)
	hashes := make([]string, 5)
	mapped := map[review.ArtifactRef]review.ArtifactRef{}
	for i := range frames {
		hashes[i] = strings.Repeat(string(rune('a'+i)), 64)
		frames[i] = review.ArtifactRef{ID: uuid.New(), SHA256: hashes[i]}
		mapped[frames[i]] = review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat(string(rune('1'+i)), 64)}
	}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: frames[0].ID, SHA256: frames[0].SHA256}, CanvasHash: rawSHA([]byte("canvas"))}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: mapped[frames[0]].ID, SHA256: mapped[frames[0]].SHA256}, CanvasHash: old.CanvasHash}
	oldID, err := canonical.Hash(map[string]any{"algorithm": "sensor_dust_log_v1", "camera_id": "raw-" + strings.Repeat("f", 64), "sensor": []int64{420, 320}, "x": 12.5, "y": 18.5, "radius": 2.0, "frames": hashes})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(map[string]any{"frames": frames, "camera_id": "raw-" + strings.Repeat("f", 64), "sensor_width": 420, "sensor_height": 320, "raw_crop": map[string]int{"x": 10, "y": 10, "width": 400, "height": 300}, "algorithm": "sensor_dust_log_v1", "candidates": []map[string]any{{"id": oldID, "sensor_x": 12.5, "sensor_y": 18.5, "radius_sensor_px": 2.0, "observed_frames": 5, "eligible_frames": 5, "recurrence": 1.0, "decision": "pending"}}, "warnings": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	codec := ImportDustCandidatesCodec{Authority: dustSeriesAuthorityStub{mapped: mapped}}
	resolve := func(ref v2input.Ref, kind string) (v2input.Ref, error) {
		if ref != old.Source || kind != "WorkingImage" {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		return target.Source, nil
	}
	prepared, err := codec.RemapImportDocument(context.Background(), "DustCandidates", raw, old, target, resolve)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Candidates []struct {
			ID string `json:"id"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(prepared, &body); err != nil || len(body.Candidates) != 1 || body.Candidates[0].ID == oldID {
		t.Fatal("dust candidate identity was not reissued", err)
	}
	if _, err := (ImportDustCandidatesCodec{}).RemapImportDocument(context.Background(), "DustCandidates", raw, old, target, resolve); err == nil {
		t.Fatal("dust series accepted without authority")
	}
}
