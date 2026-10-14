package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type detailMaskReader struct{ body []byte }

func (r detailMaskReader) Open(_ context.Context, _ objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.body)), nil
}

func TestPortraitDetailMaskStreamingChecksExactBytes(t *testing.T) {
	body := bytes.Repeat([]byte{0x3f}, 256)
	digest := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
	object := objectstore.Object{Key: "tenant/mask", VersionID: "version",
		SHA256: ref.SHA256, Bytes: int64(len(body)), ContentType: "application/octet-stream"}
	if err := hashPortraitDetailMask(context.Background(), detailMaskReader{body}, object, ref); err != nil {
		t.Fatal("valid streamed mask refused", err)
	}
	changed := append([]byte(nil), body...)
	changed[0] ^= 0xff
	if err := hashPortraitDetailMask(context.Background(), detailMaskReader{changed}, object, ref); err == nil {
		t.Fatal("changed bytes admitted")
	}
	if err := hashPortraitDetailMask(context.Background(), detailMaskReader{body[:len(body)-1]}, object, ref); err == nil {
		t.Fatal("short stream admitted")
	}
	if err := hashPortraitDetailMask(context.Background(), detailMaskReader{append(body, 0)}, object, ref); err == nil {
		t.Fatal("extra byte admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := hashPortraitDetailMask(ctx, detailMaskReader{body}, object, ref); err != context.Canceled {
		t.Fatal("cancelled stream opened", err)
	}
}
