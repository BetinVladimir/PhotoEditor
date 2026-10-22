package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type nativeReplayCompareStore struct {
	data          map[string][]byte
	opens, closes int
	closeErr      error
}

func (s *nativeReplayCompareStore) Open(_ context.Context, o objectstore.Object) (io.ReadCloser, error) {
	s.opens++
	return &nativeReplayCompareReader{Reader: bytes.NewReader(s.data[o.Key]), store: s}, nil
}
func (s *nativeReplayCompareStore) PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error) {
	panic("comparison must not publish")
}
func (s *nativeReplayCompareStore) RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error) {
	panic("comparison must not recover")
}

type nativeReplayCompareReader struct {
	*bytes.Reader
	store *nativeReplayCompareStore
}

func (r *nativeReplayCompareReader) Close() error { r.store.closes++; return r.store.closeErr }
func TestAuthoredNativeReplayPhysicalComparisonBoundaries(t *testing.T) {
	original := bytes.Repeat([]byte("actual-npy-payload"), 8000)
	h := sha256.Sum256(original)
	digest := hex.EncodeToString(h[:])
	for _, tc := range []struct {
		name                         string
		right                        []byte
		hash                         string
		cancel, closeFailure, wantOK bool
	}{
		{name: "multiple bounded chunks exact", right: original, hash: digest, wantOK: true},
		{name: "truncated", right: original[:len(original)-1], hash: digest},
		{name: "extra byte", right: append(append([]byte(nil), original...), 0), hash: digest},
		{name: "changed byte", right: append([]byte("X"), original[1:]...), hash: digest},
		{name: "wrong nominated hash", right: original, hash: string(bytes.Repeat([]byte("0"), 64))},
		{name: "cancel before reader", right: original, hash: digest, cancel: true},
		{name: "close failure refuses proof", right: original, hash: digest, closeFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &nativeReplayCompareStore{data: map[string][]byte{"a": original, "b": tc.right}}
			if tc.closeFailure {
				store.closeErr = errors.New("close")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			p := AuthoredNativeReplayPublication{Objects: store}
			a := objectstore.Object{Key: "a", Bytes: int64(len(original)), SHA256: tc.hash}
			b := a
			b.Key = "b"
			err := p.compareStreams(ctx, a, b)
			if (err == nil) != tc.wantOK {
				t.Fatalf("success=%v error=%v", tc.wantOK, err)
			}
			if tc.cancel && store.opens != 0 {
				t.Fatal("cancelled proof opened storage")
			}
			if store.closes != store.opens {
				t.Fatalf("unclosed streams %d/%d", store.closes, store.opens)
			}
		})
	}
}
