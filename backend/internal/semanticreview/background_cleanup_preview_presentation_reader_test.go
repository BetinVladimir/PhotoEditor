package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

// Exercises the common reader with background cleanup producer identity. The
// authority loader is a double; this does not qualify SQL ACL or physical PNGs.
func TestBackgroundCleanupPresentationReaderRechecksCanonicalIdentity(t *testing.T) {
	for _, mode := range []string{"valid", "task", "result", "attempt", "fence", "artifact", "sha", "version", "key", "bytes", "revoked", "cancel", "corrupt", "foreign_initial"} {
		t.Run(mode, func(t *testing.T) {
			run, target := uuid.New(), uuid.New()
			expected := uuid.NewSHA1(run, []byte("semantic.preview_background_cleanup:"+target.String()))
			raw := []byte(`{"schema_version":1}`)
			digest := sha256.Sum256(raw)
			source := previewEvidenceSource{TaskID: expected, ResultID: uuid.New(), Attempt: 1, Fence: 1,
				Object: PlanSource{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(digest[:])},
					Bytes: int64(len(raw)), MediaType: "application/json", Key: "preview/presentation", Version: "v1"}}
			if mode == "foreign_initial" {
				source.TaskID = uuid.NewSHA1(run, []byte("semantic.preview_remove_object:"+target.String()))
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			loads, reads := 0, 0
			load := func(context.Context) (previewEvidenceSource, error) {
				loads++
				current := source
				if loads == 2 {
					switch mode {
					case "task":
						current.TaskID = uuid.New()
					case "result":
						current.ResultID = uuid.New()
					case "attempt":
						current.Attempt++
					case "fence":
						current.Fence++
					case "artifact":
						current.Object.Ref.ArtifactID = uuid.New()
					case "sha":
						current.Object.Ref.SHA256 = previewParamsFixture().Plan.SHA256
					case "version":
						current.Object.Version = "v2"
					case "key":
						current.Object.Key = "foreign/presentation"
					case "bytes":
						current.Object.Bytes++
					case "revoked":
						return previewEvidenceSource{}, ErrConflict
					}
				}
				return current, nil
			}
			read := func(context.Context, PlanSource) ([]byte, error) {
				reads++
				if mode == "cancel" {
					cancel()
				}
				if mode == "corrupt" {
					return []byte(`{"schema_version":2}`), nil
				}
				return raw, nil
			}
			got, err := readBoundPreviewPresentationDocument(ctx, load, read, run, expected)
			if mode == "valid" {
				if err != nil || loads != 2 || reads != 1 || got.TaskID != expected {
					t.Fatal("canonical read failed", got, err, loads, reads)
				}
				raw[0] = '!'
				if got.raw[0] != '{' {
					t.Fatal("reader retained mutable object buffer")
				}
			} else {
				if err == nil || got.TaskID != uuid.Nil || len(got.raw) != 0 {
					t.Fatal("stale/foreign presentation escaped", got, err)
				}
				if mode == "foreign_initial" && reads != 0 {
					t.Fatal("foreign operation reached object IO")
				}
				if mode == "cancel" && err != context.Canceled {
					t.Fatal("lost cancellation", err)
				}
			}
		})
	}
}
