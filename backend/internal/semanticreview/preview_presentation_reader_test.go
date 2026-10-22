package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPresentationDocumentRechecksProducerAfterIO(t *testing.T) {
	for _, failure := range []string{"valid", "changed", "revoked", "cancel", "truncated", "corrupted"} {
		p := previewParamsFixture()
		run := uuid.New()
		e := ExposurePreviewEvidence{SchemaVersion: 1, Params: p, WorkingImage: v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Plan.SHA256}, Pixels: v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Plan.SHA256}, Algorithm: "local_exposure_exact_replay_v1", ProtectedPixelsUnchanged: true}
		raw, err := canonical.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		source := previewEvidenceSource{TaskID: uuid.NewSHA1(run, []byte("semantic.preview_local_exposure:"+p.TargetID.String())), ResultID: uuid.New(), Attempt: 1, Fence: 1, Object: PlanSource{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, Bytes: int64(len(raw)), MediaType: "application/json"}}
		calls := 0
		load := func(context.Context) (previewEvidenceSource, error) {
			calls++
			current := source
			if calls == 2 {
				if failure == "changed" {
					current.Fence++
				}
				if failure == "revoked" {
					return previewEvidenceSource{}, ErrConflict
				}
			}
			return current, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		read := func(context.Context, PlanSource) ([]byte, error) {
			if failure == "cancel" {
				cancel()
			}
			if failure == "corrupted" {
				return append([]byte("!"), raw[1:]...), nil
			}
			if failure == "truncated" {
				return raw[:len(raw)-1], nil
			}
			return raw, nil
		}
		got, err := readPreviewPresentationDocument(ctx, load, read, run, p)
		cancel()
		if (err == nil) != (failure == "valid") {
			t.Fatal(failure, err)
		}
		if failure == "valid" && (calls != 2 || got.ResultID != source.ResultID) {
			t.Fatal("missing presentation fence", got, calls)
		}
		if failure != "valid" && got.TaskID != uuid.Nil {
			t.Fatal("partial presentation escaped", failure)
		}
	}
}
