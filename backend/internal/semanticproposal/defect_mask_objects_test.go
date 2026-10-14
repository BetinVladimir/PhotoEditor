package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDefectMaskCatalogPins(t *testing.T) {
	entry := func(size int64) defectEvidenceObject {
		return defectEvidenceObject{ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, object: objectstore.Object{Key: "key", VersionID: "immutable-version", Bytes: size}}
	}
	original := defectMaskObjects{Root: entry(2), Params: entry(1048576), Pixel: entry(800065548)}
	buckets := [3]string{"photo", "photo", "photo"}
	if !validDefectMaskObjects(original, buckets, "photo") {
		t.Fatal("valid boundaries rejected")
	}
	mutations := []func(*defectMaskObjects){
		func(o *defectMaskObjects) { o.Root.ref.ArtifactID = uuid.Nil },
		func(o *defectMaskObjects) { o.Params.ref.ArtifactID = o.Root.ref.ArtifactID },
		func(o *defectMaskObjects) { o.Pixel.ref.SHA256 = "invalid" },
		func(o *defectMaskObjects) { o.Root.object.Bytes = 1048577 },
		func(o *defectMaskObjects) { o.Pixel.object.Bytes = 31 },
		func(o *defectMaskObjects) { o.Pixel.object.Bytes = 800065549 },
		func(o *defectMaskObjects) { o.Params.object.VersionID = "null" },
		func(o *defectMaskObjects) { o.Pixel.object.Key = "" },
	}
	for i, mutate := range mutations {
		changed := original
		mutate(&changed)
		if validDefectMaskObjects(changed, buckets, "photo") {
			t.Fatal("invalid metadata accepted", i)
		}
	}
	buckets[1] = "foreign"
	if validDefectMaskObjects(original, buckets, "photo") {
		t.Fatal("foreign bucket accepted")
	}
}
