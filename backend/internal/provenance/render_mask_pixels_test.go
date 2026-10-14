package provenance

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type signingMaskCatalogFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, review.MaskRef) (objectstore.Object, error)

func (f signingMaskCatalogFunc) LookupSigningRenderMask(ctx context.Context, w auth.Identity, r ReadySigningExport, t CanonicalSigningTask, d v2input.Ref, m review.MaskRef) (objectstore.Object, error) {
	return f(ctx, w, r, t, d, m)
}

func TestSigningRenderMaskCountPinsPhysicalVersion(t *testing.T) {
	header := "{'descr': '<f4', 'fortran_order': False, 'shape': (1, 2), }"
	header += strings.Repeat(" ", (64-(10+len(header)+1)%64)%64) + "\n"
	raw := append([]byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0}, byte(len(header)), byte(len(header)>>8))
	raw = append(raw, []byte(header)...)
	raw = append(raw, make([]byte, 8)...)
	binary.LittleEndian.PutUint32(raw[len(raw)-4:], 0x3f800000)
	for _, mode := range []string{"valid", "octet", "changed_version", "hash", "truncated", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			sha := bodyDigest(raw)
			mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: sha}, Canvas: review.Canvas{Width: 2, Height: 1}, Selector: "generated", Confidence: 1}
			task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: sha, ResultDigest: sha, Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Inputs: json.RawMessage(`[]`), Outputs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{}`)}
			object := objectstore.Object{Key: "mask", VersionID: "v1", SHA256: sha, Bytes: int64(len(raw)), ContentType: "application/x-npy"}
			if mode == "octet" {
				object.ContentType = "application/octet-stream"
			}
			calls, opens := 0, 0
			catalog := signingMaskCatalogFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, review.MaskRef) (objectstore.Object, error) {
				calls++
				value := object
				if mode == "changed_version" && calls == 2 {
					value.VersionID = "v2"
				}
				return value, nil
			})
			objects := signingDocumentObjectsFunc(func(_ context.Context, expected objectstore.Object) (io.ReadCloser, error) {
				opens++
				if expected != object {
					t.Fatal("object version changed")
				}
				body := append([]byte(nil), raw...)
				if mode == "hash" {
					body[len(body)-1] ^= 1
				}
				if mode == "truncated" {
					body = body[:len(body)-1]
				}
				return io.NopCloser(bytes.NewReader(body)), nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			count, err := (BoundSigningRenderMaskCounter{Catalog: catalog, Objects: objects}).CountSigningRenderMask(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New()}, task, v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, mask)
			if mode == "valid" || mode == "octet" {
				if err != nil || count != 1 || calls != 2 || opens != 1 {
					t.Fatal("valid count refused", count, err)
				}
				return
			}
			if err == nil || count != 0 {
				t.Fatal("unverified mask count exposed", count, err)
			}
			if mode == "cancelled" && (calls != 0 || opens != 0) {
				t.Fatal("cancelled mask read")
			}
		})
	}
}
