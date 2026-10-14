package interchange

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type personOriginPublishStore struct {
	reservation       artifactjson.Reservation
	reserve, complete int
}

func (s *personOriginPublishStore) ReservePersonOrigin(_ context.Context, _ PortablePersonOriginExpectation, _ PreparedPersonOriginAttachment) (artifactjson.Reservation, error) {
	s.reserve++
	return s.reservation, nil
}
func (s *personOriginPublishStore) CompletePersonOrigin(_ context.Context, _ PortablePersonOriginExpectation,
	attachment PreparedPersonOriginAttachment, _ artifactjson.Reservation, _ objectstore.Object) (v2input.Ref, error) {
	s.complete++
	return attachment.Ref, nil
}

type personOriginPublishedReader struct{ body []byte }

func (r personOriginPublishedReader) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.body)), nil
}

func TestPersonOriginPublisherRequiresSignedVersionedPhysicalReceipt(t *testing.T) {
	raw, scope, keys := portablePersonOriginFixture(t)
	path := "origins/" + scope.EntryID.String() + "/" + scope.MaskRef.ArtifactID.String() + ".json"
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(scope.EntryID, []byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String())), SHA256: rawSHA(raw)}
	attachment := PreparedPersonOriginAttachment{Ref: ref, Path: path, Body: raw,
		Mapping: importBundlePersonOrigin{EntryID: scope.EntryID, Recipe: scope.Recipe,
			Override: scope.Override, MaskRef: scope.MaskRef, MaskPixels: scope.MaskPixels, Path: path, SHA: ref.SHA256}}
	for _, mode := range []string{"valid", "replay", "tampered", "current", "stored"} {
		t.Run(mode, func(t *testing.T) {
			store := &personOriginPublishStore{reservation: artifactjson.Reservation{ArtifactID: ref.ArtifactID, Token: uuid.New()}}
			if mode == "replay" {
				store.reservation.Ready = &artifactjson.Ref{ID: ref.ArtifactID, SHA256: ref.SHA256}
			}
			objects := &createStorageObjectsStub{}
			current := &originCurrentStub{deny: mode == "current"}
			candidate := attachment
			candidate.Body = append([]byte(nil), raw...)
			if mode == "tampered" {
				candidate.Body[0] = '!'
			}
			stored := append([]byte(nil), raw...)
			if mode == "stored" {
				stored[0] = '!'
			}
			publisher := ExportPersonOriginAttachmentPublisher{Tenant: uuid.New(), Run: uuid.New(),
				Repository: store, Objects: objects, Reader: personOriginPublishedReader{body: stored},
				Current: current, Trusted: keys}
			got, err := publisher.Publish(context.Background(), scope, candidate)
			if mode == "valid" || mode == "replay" {
				if err != nil || got != ref || current.calls != 2 ||
					(mode == "valid" && (store.complete != 1 || objects.put != 1)) ||
					(mode == "replay" && (store.complete != 0 || objects.put != 0 || objects.recover != 1)) {
					t.Fatal("signed receipt not published", err)
				}
			} else if err == nil || store.complete != 0 {
				t.Fatal("unqualified receipt completed", mode)
			}
		})
	}
}
