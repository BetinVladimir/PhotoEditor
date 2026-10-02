package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type sessionImageBytes []byte

func (b sessionImageBytes) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b)), nil
}

func TestReadSessionWorkingImageVerifiesVersionedBytesAndCanvas(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if err := json.Unmarshal(raw, &fixtures); err != nil || len(fixtures) == 0 {
		t.Fatal(err)
	}
	body := fixtures[0]
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	canvasSHA, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	ref := Ref{uuid.New(), sha}
	object := objectstore.Object{Key: "test/image", VersionID: "v1",
		SHA256: sha, Bytes: int64(len(body)), ContentType: "application/json"}
	got, err := readSessionWorkingImage(context.Background(), sessionImageBytes(body),
		object, ref, canvasSHA)
	if err != nil || got.Pixels != image.Pixels {
		t.Fatalf("verified source: %v", err)
	}
	if _, err := readSessionWorkingImage(context.Background(), sessionImageBytes(body),
		object, ref, strings.Repeat("0", 64)); err == nil {
		t.Fatal("accepted different canvas")
	}
	tampered := append([]byte(nil), body...)
	tampered[len(tampered)-2] = '0'
	if _, err := readSessionWorkingImage(context.Background(), sessionImageBytes(tampered),
		object, ref, canvasSHA); err == nil {
		t.Fatal("accepted changed object bytes")
	}
}
