package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"photoeditor/backend/internal/objectstore"
)

func TestPublishedDraftObjectRequiresExactImmutableReceipt(t *testing.T) {
	key := "tenants/owner/drafts/document"
	digest := strings.Repeat("a", 64)
	good := objectstore.Object{Key: key, VersionID: "version-1", SHA256: digest, Bytes: 42, ContentType: "application/json"}
	if !validPublishedDraftObject(good, key, digest, 42) {
		t.Fatal("exact immutable receipt rejected")
	}
	for name, change := range map[string]func(*objectstore.Object){
		"key":     func(v *objectstore.Object) { v.Key = "tenants/other/drafts/document" },
		"version": func(v *objectstore.Object) { v.VersionID = "null" },
		"sha":     func(v *objectstore.Object) { v.SHA256 = strings.Repeat("b", 64) },
		"size":    func(v *objectstore.Object) { v.Bytes++ },
		"mime":    func(v *objectstore.Object) { v.ContentType = "text/plain" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := good
			change(&bad)
			if validPublishedDraftObject(bad, key, digest, 42) {
				t.Fatal("substituted storage receipt accepted")
			}
		})
	}
}

type draftReplayReader struct {
	*bytes.Reader
	closed bool
}

func (r *draftReplayReader) Close() error { r.closed = true; return nil }

func TestExactDraftReplayChecksCompletePhysicalBytesAndCloses(t *testing.T) {
	body := []byte(`{"recipe":true}`)
	sum := sha256.Sum256(body)
	object := objectstore.Object{Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
	for name, raw := range map[string][]byte{
		"exact":   body,
		"short":   body[:len(body)-1],
		"extra":   append(bytes.Clone(body), 'x'),
		"corrupt": append([]byte{'X'}, body[1:]...),
	} {
		t.Run(name, func(t *testing.T) {
			reader := &draftReplayReader{Reader: bytes.NewReader(raw)}
			err := verifyExactDraftObjectStream(context.Background(), reader, object)
			if (err == nil) != (name == "exact") || !reader.closed {
				t.Fatal("physical replay verification mismatch", err, reader.closed)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &draftReplayReader{Reader: bytes.NewReader(body)}
	if err := verifyExactDraftObjectStream(ctx, reader, object); err == nil || !reader.closed {
		t.Fatal("cancelled replay read accepted or leaked", err)
	}
	if err := verifyExactDraftObjectStream(context.Background(), io.NopCloser(bytes.NewReader(body)), objectstore.Object{Bytes: object.Bytes, SHA256: strings.Repeat("f", 64)}); err == nil {
		t.Fatal("substituted replay digest accepted")
	}
}
