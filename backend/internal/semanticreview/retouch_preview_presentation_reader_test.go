package semanticreview

import (
	"context"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

// Tests the shared byte/producer fence with synthetic JSON; document semantics
// and SQL provenance are independently checked by the retouch linkage reader.
func TestRetouchPresentationDocumentRechecksProducerAfterIO(t *testing.T) {
	for _, failure := range []string{"valid", "changed", "revoked", "cancel", "truncated", "corrupted", "wrong_operation", "buffer_reuse", "result", "attempt", "task", "artifact", "sha", "size", "bucket", "key", "version", "media"} {
		p := previewParamsFixture()
		run := uuid.New()
		e := ExposurePreviewEvidence{SchemaVersion: 1, Params: p, WorkingImage: v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Plan.SHA256}, Pixels: v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Plan.SHA256}, Algorithm: "local_exposure_exact_replay_v1", ProtectedPixelsUnchanged: true}
		raw, err := canonical.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		expectedRaw := append([]byte(nil), raw...)
		sum := sha256.Sum256(raw)
		source := previewEvidenceSource{TaskID: uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+p.TargetID.String())), ResultID: uuid.New(), Attempt: 1, Fence: 1, Object: PlanSource{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, Bytes: int64(len(raw)), MediaType: "application/json"}}
		calls := 0
		load := func(context.Context) (previewEvidenceSource, error) {
			calls++
			if failure == "wrong_operation" {
				source.TaskID = uuid.NewSHA1(run, []byte("semantic.preview_local_exposure:"+p.TargetID.String()))
			}
			current := source
			if calls == 2 {
				if failure == "buffer_reuse" { raw[0] = '!' }
				switch failure {
				case "result": current.ResultID = uuid.New()
				case "attempt": current.Attempt++
				case "task": current.TaskID = uuid.New()
				case "artifact": current.Object.Ref.ArtifactID = uuid.New()
				case "sha": current.Object.Ref.SHA256 = p.Plan.SHA256
				case "size": current.Object.Bytes++
				case "bucket": current.Object.Bucket = "changed"
				case "key": current.Object.Key = "changed"
				case "version": current.Object.Version = "changed"
				case "media": current.Object.MediaType = "image/png"
				}
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
		got, err := readBoundPreviewPresentationDocument(ctx, load, read, run, uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+p.TargetID.String())))
		cancel()
		success := failure == "valid" || failure == "buffer_reuse"
		if (err == nil) != success {
			t.Fatal(failure, err)
		}
		if success && (calls != 2 || got.ResultID != source.ResultID || !bytes.Equal(got.raw, expectedRaw)) {
			t.Fatal("missing presentation fence", got, calls)
		}
		if !success && got.TaskID != uuid.Nil {
			t.Fatal("partial presentation escaped", failure)
		}
	}
}
