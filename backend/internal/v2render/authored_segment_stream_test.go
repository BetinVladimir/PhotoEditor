package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type authoredSegmentStreamReader struct {
	data              *bytes.Reader
	readErr, closeErr error
	closeCount        int
	cancel            context.CancelFunc
	maxRead           int
}

func (r *authoredSegmentStreamReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.maxRead = len(p)
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.readErr != nil {
		return 0, r.readErr
	}
	return r.data.Read(p)
}
func (r *authoredSegmentStreamReader) Close() error { r.closeCount++; return r.closeErr }

type authoredSegmentStreamObjects struct {
	reader *authoredSegmentStreamReader
	opens  int
	object objectstore.Object
}

func (o *authoredSegmentStreamObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	o.opens++
	o.object = obj
	return o.reader, nil
}
func authoredSegmentStreamFixture(data []byte) (v2input.Ref, objectstore.Object) {
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, objectstore.Object{Key: "owned-version", VersionID: "immutable-v1", SHA256: sha, Bytes: int64(len(data)), ContentType: "application/x-npy"}
}

func TestAuthoredSegmentStreamReadsExactBytesWithBoundedBuffer(t *testing.T) {
	data := bytes.Repeat([]byte("canonical-array-bytes"), 10000)
	ref, obj := authoredSegmentStreamFixture(data)
	reader := &authoredSegmentStreamReader{data: bytes.NewReader(data)}
	objects := &authoredSegmentStreamObjects{reader: reader}
	if err := verifyAuthoredSegmentStream(context.Background(), objects, obj, ref); err != nil {
		t.Fatal(err)
	}
	if objects.opens != 1 || objects.object != obj || reader.closeCount != 1 || reader.maxRead > 65536 {
		t.Fatalf("stream ownership/bound changed: %+v %+v", objects, reader)
	}
}

func TestAuthoredSegmentStreamRefusesPhysicalCorruptionAndLength(t *testing.T) {
	original := []byte("canonical-array")
	ref, obj := authoredSegmentStreamFixture(original)
	for name, data := range map[string][]byte{"corrupt": []byte("Canonical-array"), "short": original[:len(original)-1], "extra": append(append([]byte(nil), original...), 0)} {
		t.Run(name, func(t *testing.T) {
			reader := &authoredSegmentStreamReader{data: bytes.NewReader(data)}
			err := verifyAuthoredSegmentStream(context.Background(), &authoredSegmentStreamObjects{reader: reader}, obj, ref)
			if !errors.Is(err, v2input.ErrInvalid) || reader.closeCount != 1 {
				t.Fatalf("corruption accepted/stream leaked: %v %d", err, reader.closeCount)
			}
		})
	}
}

func TestAuthoredSegmentStreamPreservesIOAndCancellationErrors(t *testing.T) {
	data := []byte("canonical-array")
	ref, obj := authoredSegmentStreamFixture(data)
	readErr := errors.New("read-sentinel")
	closeErr := errors.New("close-sentinel")
	for _, test := range []struct {
		name              string
		read, close, want error
	}{{"read", readErr, closeErr, readErr}, {"close", nil, closeErr, closeErr}} {
		t.Run(test.name, func(t *testing.T) {
			reader := &authoredSegmentStreamReader{data: bytes.NewReader(data), readErr: test.read, closeErr: test.close}
			err := verifyAuthoredSegmentStream(context.Background(), &authoredSegmentStreamObjects{reader: reader}, obj, ref)
			if !errors.Is(err, test.want) || reader.closeCount != 1 {
				t.Fatalf("IO identity/close: %v %d", err, reader.closeCount)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	objects := &authoredSegmentStreamObjects{}
	if err := verifyAuthoredSegmentStream(ctx, objects, obj, ref); !errors.Is(err, context.Canceled) || objects.opens != 0 {
		t.Fatalf("pre-open cancellation: %v %d", err, objects.opens)
	}
	ctx, cancel = context.WithCancel(context.Background())
	reader := &authoredSegmentStreamReader{data: bytes.NewReader(data), cancel: cancel}
	err := verifyAuthoredSegmentStream(ctx, &authoredSegmentStreamObjects{reader: reader}, obj, ref)
	if !errors.Is(err, context.Canceled) || reader.closeCount != 1 {
		t.Fatalf("midstream cancellation: %v %d", err, reader.closeCount)
	}
}
