package semanticproposal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type transferReadStore struct {
	bodies   map[string][]byte
	opened   []objectstore.Object
	closed   int
	closeErr error
}
type transferReadStream struct {
	*bytes.Reader
	store *transferReadStore
}

func (s *transferReadStream) Close() error { s.store.closed++; return s.store.closeErr }
func (s *transferReadStore) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	s.opened = append(s.opened, obj)
	return &transferReadStream{bytes.NewReader(s.bodies[obj.Key]), s}, nil
}

func TestTransferDocumentReadsOwnExactVersionedBundle(t *testing.T) {
	store := &transferReadStore{bodies: map[string][]byte{}}
	pins := make([]PinnedDocument, 5)
	for i := range pins {
		body := []byte(fmt.Sprintf(`{"document":%d}`, i))
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		key := fmt.Sprintf("fixture/doc-%d", i)
		pins[i] = PinnedDocument{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest}, Object: objectstore.Object{Key: key, VersionID: fmt.Sprintf("version-%d", i), SHA256: digest, Bytes: int64(len(body)), ContentType: "application/json"}}
		store.bodies[key] = body
	}
	bundle := TransferDocumentPins{pins[0], pins[1], pins[2], pins[3], pins[4]}
	got, err := ReadTransferDocuments(context.Background(), store, bundle)
	if err != nil || store.closed != 5 || len(store.opened) != 5 {
		t.Fatal("bundle read", err)
	}
	bodies := [][]byte{got.Reference, got.Target, got.SelectedMask, got.SourceProtection, got.TargetProtection}
	for i, body := range bodies {
		if !bytes.Equal(body, store.bodies[pins[i].Object.Key]) || store.opened[i] != pins[i].Object {
			t.Fatal("lost body/version", i)
		}
	}
	got.Reference[0] = '!'
	if store.bodies[pins[0].Object.Key][0] != '{' {
		t.Fatal("borrowed object buffer")
	}
	for failAt := range pins {
		original := store.bodies[pins[failAt].Object.Key]
		store.bodies[pins[failAt].Object.Key] = append(append([]byte(nil), original...), ' ')
		store.opened = nil
		store.closed = 0
		got, err := ReadTransferDocuments(context.Background(), store, bundle)
		if !errors.Is(err, v2input.ErrInvalid) || len(store.opened) != failAt+1 || store.closed != failAt+1 || got.Reference != nil || got.Target != nil || got.SelectedMask != nil || got.SourceProtection != nil || got.TargetProtection != nil {
			t.Fatal("partial damaged bundle", failAt, err)
		}
		store.bodies[pins[failAt].Object.Key] = original
	}
	store.opened = nil
	store.closed = 0
	store.closeErr = io.ErrUnexpectedEOF
	if _, err := ReadTransferDocuments(context.Background(), store, bundle); !errors.Is(err, io.ErrUnexpectedEOF) || store.closed != 1 {
		t.Fatal("close error", err)
	}
	store.opened = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadTransferDocuments(ctx, store, bundle); !errors.Is(err, context.Canceled) || len(store.opened) != 0 {
		t.Fatal("cancelled bundle", err)
	}
}
