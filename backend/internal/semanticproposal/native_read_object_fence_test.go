package semanticproposal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"testing"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type nativeFenceObjectReader struct {
	open func(context.Context, objectstore.Object) (io.ReadCloser, error)
}

func (r nativeFenceObjectReader) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	return r.open(ctx, obj)
}

type nativeFenceReadCloser struct {
	reader   io.Reader
	closes   int
	closeErr error
}

func (r *nativeFenceReadCloser) Read(p []byte) (int, error) { return r.reader.Read(p) }
func (r *nativeFenceReadCloser) Close() error               { r.closes++; return r.closeErr }
func nativeFencePin(body []byte) objectstore.Object {
	sum := sha256.Sum256(body)
	return objectstore.Object{Key: "exact/object", VersionID: "version-1", Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:]), ContentType: "application/octet-stream"}
}
func TestNativeReadObjectFenceRejectsChangedBytesWithoutLocatorChange(t *testing.T) {
	original := []byte("verified bytes")
	pin := nativeFencePin(original)
	for _, tc := range []struct {
		name  string
		body  []byte
		valid bool
	}{
		{"unchanged", original, true},
		{"same_size_corruption", []byte("modified bytes"), false},
		{"short", original[:len(original)-1], false},
		{"trailing_byte", append(append([]byte(nil), original...), 0), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := &nativeFenceReadCloser{reader: bytes.NewReader(tc.body)}
			reader := nativeFenceObjectReader{open: func(_ context.Context, got objectstore.Object) (io.ReadCloser, error) {
				if got != pin {
					t.Fatal("locator substituted")
				}
				return stream, nil
			}}
			err := verifyNativeReadObject(context.Background(), reader, pin)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if !tc.valid && !errors.Is(err, v2input.ErrInvalid) {
				t.Fatalf("not a physical refusal: %v", err)
			}
			if stream.closes != 1 {
				t.Fatalf("stream closes=%d", stream.closes)
			}
		})
	}
}
func TestNativeReadObjectFenceRejectsInvalidPinsBeforeIO(t *testing.T) {
	pin := nativeFencePin([]byte("body"))
	edits := []func(*objectstore.Object){
		func(p *objectstore.Object) { p.Key = "" }, func(p *objectstore.Object) { p.VersionID = "" },
		func(p *objectstore.Object) { p.VersionID = "null" }, func(p *objectstore.Object) { p.Bytes = 0 },
		func(p *objectstore.Object) { p.Bytes = -1 }, func(p *objectstore.Object) { p.Bytes = math.MaxInt64 },
		func(p *objectstore.Object) { p.SHA256 = "bad" }, func(p *objectstore.Object) { p.ContentType = "" },
	}
	for i, edit := range edits {
		current := pin
		edit(&current)
		opened := false
		reader := nativeFenceObjectReader{open: func(context.Context, objectstore.Object) (io.ReadCloser, error) { opened = true; return nil, nil }}
		if err := verifyNativeReadObject(context.Background(), reader, current); !errors.Is(err, v2input.ErrInvalid) || opened {
			t.Fatalf("case%d opened=%v err=%v", i, opened, err)
		}
	}
	if err := verifyNativeReadObject(context.Background(), nil, pin); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
}

type nativeFenceErrorRead struct{ err error }

func (r nativeFenceErrorRead) Read([]byte) (int, error) { return 0, r.err }
func TestNativeReadObjectFencePreservesStreamFailuresAndCloses(t *testing.T) {
	failure := errors.New("physical read failed")
	closeFailure := errors.New("physical close failed")
	for _, tc := range []struct {
		name                    string
		read                    io.Reader
		openErr, closeErr, want error
	}{
		{"read", nativeFenceErrorRead{failure}, nil, nil, failure},
		{"close", bytes.NewReader([]byte("body")), nil, closeFailure, closeFailure},
		{"partial_open", bytes.NewReader(nil), failure, closeFailure, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := &nativeFenceReadCloser{reader: tc.read, closeErr: tc.closeErr}
			reader := nativeFenceObjectReader{open: func(context.Context, objectstore.Object) (io.ReadCloser, error) { return stream, tc.openErr }}
			err := verifyNativeReadObject(context.Background(), reader, nativeFencePin([]byte("body")))
			if !errors.Is(err, tc.want) || stream.closes != 1 {
				t.Fatalf("err=%v closes=%d", err, stream.closes)
			}
			if tc.closeErr != nil && !errors.Is(err, tc.closeErr) {
				t.Fatalf("lost close failure: %v", err)
			}
		})
	}
	nilReader := nativeFenceObjectReader{open: func(context.Context, objectstore.Object) (io.ReadCloser, error) { return nil, nil }}
	if err := verifyNativeReadObject(context.Background(), nilReader, nativeFencePin([]byte("body"))); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
}

type nativeFenceCancelRead struct{ cancel context.CancelFunc }

func (r nativeFenceCancelRead) Read(p []byte) (int, error) {
	copy(p, "body")
	r.cancel()
	return 4, nil
}
func TestNativeReadObjectFenceCancellationClosesAndDoesNotAccept(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &nativeFenceReadCloser{reader: nativeFenceCancelRead{cancel: cancel}}
	reader := nativeFenceObjectReader{open: func(context.Context, objectstore.Object) (io.ReadCloser, error) { return stream, nil }}
	if err := verifyNativeReadObject(ctx, reader, nativeFencePin([]byte("body"))); !errors.Is(err, context.Canceled) || stream.closes != 1 {
		t.Fatalf("err=%v closes=%d", err, stream.closes)
	}
	opened := false
	reader.open = func(context.Context, objectstore.Object) (io.ReadCloser, error) { opened = true; return nil, nil }
	if err := verifyNativeReadObject(ctx, reader, nativeFencePin([]byte("body"))); !errors.Is(err, context.Canceled) || opened {
		t.Fatalf("err=%v opened=%v", err, opened)
	}
}

// An object which never reaches EOF must still be bounded by its declared size.
type nativeFenceEndlessRead struct{ read int }

func (r *nativeFenceEndlessRead) Read(p []byte) (int, error) {
	clear(p)
	r.read += len(p)
	return len(p), nil
}
func TestNativeReadObjectFenceBoundsPhysicalRead(t *testing.T) {
	source := &nativeFenceEndlessRead{}
	stream := &nativeFenceReadCloser{reader: source}
	reader := nativeFenceObjectReader{open: func(context.Context, objectstore.Object) (io.ReadCloser, error) { return stream, nil }}
	pin := nativeFencePin(make([]byte, 16))
	if err := verifyNativeReadObject(context.Background(), reader, pin); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	if source.read != 17 || stream.closes != 1 {
		t.Fatalf("read=%d closes=%d", source.read, stream.closes)
	}
}
