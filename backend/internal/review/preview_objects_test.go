package review

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

func TestCandidatePreviewObjectsRequireDistinctVersionedPins(t *testing.T) {
	for _, mode := range []string{"valid", "bucket", "alias", "version", "sha", "manifest_size", "image_size", "mime", "key"} {
		t.Run(mode, func(t *testing.T) {
			sha := strings.Repeat("a", 64)
			value := CandidatePreviewObjects{Source: ArtifactRef{ID: uuid.New(), SHA256: sha}, Manifest: ArtifactRef{ID: uuid.New(), SHA256: sha}, Derivative: ArtifactRef{ID: uuid.New(), SHA256: sha}, ManifestObject: objectstore.Object{Key: "manifest", VersionID: "v1", SHA256: sha, ContentType: "application/json", Bytes: 100}, DerivativeObject: objectstore.Object{Key: "image", VersionID: "v2", SHA256: sha, ContentType: "image/jpeg", Bytes: 100}}
			bucket := "test"
			switch mode {
			case "bucket":
				bucket = "other"
			case "alias":
				value.Manifest.ID = value.Source.ID
			case "version":
				value.DerivativeObject.VersionID = "null"
			case "sha":
				value.ManifestObject.SHA256 = strings.Repeat("b", 64)
			case "manifest_size":
				value.ManifestObject.Bytes = 1048577
			case "image_size":
				value.DerivativeObject.Bytes = 52*1048576 + 1
			case "mime":
				value.DerivativeObject.ContentType = "application/json"
			case "key":
				value.ManifestObject.Key = ""
			}
			err := validateCandidatePreviewObjects(value, "test", "test", bucket)
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid object pins accepted")
			}
		})
	}
}
