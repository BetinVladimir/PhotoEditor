package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestMultiPreviewOutputMetadataGuards(t *testing.T) {
	value := multiPreviewObject{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Object: objectstore.Object{Key: "preview", VersionID: "v1", Bytes: 10, ContentType: "image/webp"}, Width: 1600, Height: 1000}
	if err := validateMultiPreviewObject(value, "photos", "photos", "preview1600"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"version", "width", "mime", "sha", "bytes"} {
		bad := value
		switch kind {
		case "version":
			bad.Object.VersionID = "null"
		case "width":
			bad.Width = 1601
		case "mime":
			bad.Object.ContentType = "application/json"
		case "sha":
			bad.Ref.SHA256 = "bad"
		case "bytes":
			bad.Object.Bytes = 54525953
		}
		if err := validateMultiPreviewObject(bad, "photos", "photos", "preview1600"); err == nil {
			t.Fatal("invalid object accepted", kind)
		}
	}
	if _, err := (MultiProposalService{}).Preview(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), "optical.panorama", "preview1600"); err == nil {
		t.Fatal("unconfigured preview accepted")
	}
}

func TestMultiPreviewSignedURLBindsExactObjectVersion(t *testing.T) {
	for _, raw := range []string{
		"https://objects.example/preview", "https://objects.example/preview?versionId=other",
		"https://objects.example/preview?versionId=v1&versionId=v2",
		"file:///preview?versionId=v1", "https://user@objects.example/preview?versionId=v1",
	} {
		if validVersionedPreviewURL(raw, "v1") {
			t.Fatalf("accepted unbound URL: %s", raw)
		}
	}
	if !validVersionedPreviewURL("https://objects.example/preview?versionId=v1&X-Amz-Signature=abc", "v1") {
		t.Fatal("rejected exactly versioned signed URL")
	}
}
