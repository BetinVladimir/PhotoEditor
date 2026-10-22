package v2sessions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

type sceneReaderStub struct {
	calls int
	onRead func()
}
func (s *sceneReaderStub) Open(_ context.Context, _ objectstore.Object) (io.ReadCloser, error) {
	s.calls++
	if s.onRead != nil { s.onRead() }
	return io.NopCloser(bytes.NewReader([]byte("{}"))), nil
}
func sceneReaderSource() sceneObject {
	sum := sha256.Sum256([]byte("{}"))
	return sceneObject{ID: uuid.New(), Bucket: "photos", Object: objectstore.Object{
		Key: "document", VersionID: "v1", ContentType: "application/json",
		Bytes: 2, SHA256: hex.EncodeToString(sum[:]),
	}}
}
func TestSceneReaderRequiresPinnedObjectBeforeIO(t *testing.T) {
	for _, version := range []string{"", "null"} {
		t.Run("version="+version, func(t *testing.T) {
			store := &sceneReaderStub{}
			source := sceneReaderSource(); source.Object.VersionID = version
			raw, err := (PlanMaterializer{Objects: store, Bucket: "photos"}).readSceneObject(context.Background(), source)
			if err == nil || raw != nil || store.calls != 0 { t.Fatalf("unpinned read: %q %v %d", raw, err, store.calls) }
		})
	}
}
func TestSceneReaderDiscardsBytesWhenCancelledDuringIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &sceneReaderStub{onRead: cancel}
	raw, err := (PlanMaterializer{Objects: store, Bucket: "photos"}).readSceneObject(ctx, sceneReaderSource())
	if !errors.Is(err, context.Canceled) || raw != nil { t.Fatalf("cancelled read: %q %v", raw, err) }
}
func TestSceneReaderReturnsExactPinnedCanonicalBytes(t *testing.T) {
	store := &sceneReaderStub{}
	raw, err := (PlanMaterializer{Objects: store, Bucket: "photos"}).readSceneObject(context.Background(), sceneReaderSource())
	if err != nil || string(raw) != "{}" || store.calls != 1 { t.Fatalf("valid read: %q %v", raw, err) }
}
