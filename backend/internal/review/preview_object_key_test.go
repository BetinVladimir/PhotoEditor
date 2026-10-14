package review

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

func TestPreviewLinkMustMatchFreshCatalogObject(t *testing.T) {
	ref := ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	object := objectstore.Object{Key: "tenant/preview/image", VersionID: "v1", SHA256: ref.SHA256,
		ContentType: "image/jpeg", Bytes: 100}
	link := CandidatePreviewLink{Artifact: ref, Bytes: 100, MediaType: "image/jpeg",
		ExpiresAt: time.Now().Add(time.Minute)}
	if !previewLinkObjectMatches(link, object, ref, "v1") {
		t.Fatal("exact catalog binding refused")
	}
	for _, mutate := range []func(*CandidatePreviewLink, *objectstore.Object, *ArtifactRef){
		func(_ *CandidatePreviewLink, o *objectstore.Object, _ *ArtifactRef) { o.Key = "" },
		func(_ *CandidatePreviewLink, o *objectstore.Object, _ *ArtifactRef) { o.VersionID = "v2" },
		func(_ *CandidatePreviewLink, o *objectstore.Object, _ *ArtifactRef) {
			o.SHA256 = strings.Repeat("b", 64)
		},
		func(l *CandidatePreviewLink, _ *objectstore.Object, _ *ArtifactRef) { l.Bytes++ },
		func(l *CandidatePreviewLink, _ *objectstore.Object, _ *ArtifactRef) { l.MediaType = "image/png" },
		func(l *CandidatePreviewLink, _ *objectstore.Object, _ *ArtifactRef) { l.Artifact.ID = uuid.New() },
		func(_ *CandidatePreviewLink, _ *objectstore.Object, r *ArtifactRef) {
			r.SHA256 = strings.Repeat("b", 64)
		},
	} {
		changedLink, changedObject, changedRef := link, object, ref
		mutate(&changedLink, &changedObject, &changedRef)
		if previewLinkObjectMatches(changedLink, changedObject, changedRef, "v1") {
			t.Fatal("changed catalog binding accepted", changedLink, changedObject, changedRef)
		}
	}
}
