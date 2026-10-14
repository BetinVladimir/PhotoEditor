package interchange

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type dustReviewReader struct{ raw []byte }

func (r dustReviewReader) ReadPreparedImportDocument(_ context.Context, entry v2input.Entry, _ int64) ([]byte, error) {
	if entry.Contract != "DustCandidates" || entry.Ref.SHA256 != rawSHA(r.raw) {
		return nil, v2input.ErrInvalid
	}
	return append([]byte(nil), r.raw...), nil
}

type dustReviewCurrentStub struct{ forged bool }

func (s dustReviewCurrentStub) ReissueImportedDustReview(_ context.Context, _, current review.DustSelection, _, _ v2input.SessionVersionBinding) (review.DustSelection, error) {
	if s.forged {
		current.Accepted = []string{rawSHA([]byte("unknown"))}
	}
	return current, nil
}
func (dustReviewCurrentStub) QualifyPreparedDustReview(context.Context, review.DustSelection, v2input.SessionVersionBinding) error {
	return nil
}

func TestBoundDustReviewRejectsUnknownCandidate(t *testing.T) {
	frames := make([]review.ArtifactRef, 5)
	hashes := make([]string, 5)
	for i := range frames {
		hashes[i] = strings.Repeat(string(rune('a'+i)), 64)
		frames[i] = review.ArtifactRef{ID: uuid.New(), SHA256: hashes[i]}
	}
	id, err := canonical.Hash(map[string]any{"algorithm": "sensor_dust_log_v1", "camera_id": "raw-" + strings.Repeat("f", 64), "sensor": []int64{420, 320}, "x": 12.5, "y": 18.5, "radius": 2.0, "frames": hashes})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(map[string]any{"frames": frames, "camera_id": "raw-" + strings.Repeat("f", 64), "sensor_width": 420, "sensor_height": 320, "raw_crop": map[string]int{"x": 10, "y": 10, "width": 400, "height": 300}, "algorithm": "sensor_dust_log_v1", "candidates": []map[string]any{{"id": id, "sensor_x": 12.5, "sensor_y": 18.5, "radius_sensor_px": 2.0, "observed_frames": 5, "eligible_frames": 5, "recurrence": 1.0, "decision": "pending"}}, "warnings": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	proposal := review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(raw)}
	selection := review.DustSelection{Version: "sensor_dust_review_v1", Proposal: proposal, Base: frames[0], Revision: 2, Accepted: []string{id}, Rejected: []string{}}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: frames[0].ID, SHA256: frames[0].SHA256}, CanvasHash: rawSHA([]byte("canvas"))}
	authority := BoundImportDustReviewAuthority{Documents: dustReviewReader{raw: raw}, Series: dustSeriesAuthorityStub{}, Current: dustReviewCurrentStub{}, Target: target}
	if _, err := authority.ReissueImportedDustReview(context.Background(), selection, selection, target, target); err != nil {
		t.Fatal("known candidate rejected", err)
	}
	authority.Current = dustReviewCurrentStub{forged: true}
	if _, err := authority.ReissueImportedDustReview(context.Background(), selection, selection, target, target); err == nil {
		t.Fatal("unknown candidate accepted")
	}
}
