package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPerspectiveProposalInvalidScopeAndCancellation(t *testing.T) {
	service := PerspectiveProposalService{Bucket: "photos"}
	if _, err := service.Describe(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("missing authority accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Describe(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), uuid.New()); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestPerspectivePreviewLocatorValidation(t *testing.T) {
	var v perspectivePreviewObject
	v.Ref.ArtifactID = uuid.New()
	v.Ref.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	v.Object.SHA256 = v.Ref.SHA256
	v.Object.Key = "tenants/result"
	v.Object.VersionID = "immutable"
	v.Object.ContentType = "image/webp"
	v.Object.Bytes = 100
	v.Width = 1600
	v.Height = 900
	if err := validatePerspectivePreviewObject(v, "photos", "photos", "preview1600"); err != nil {
		t.Fatal(err)
	}
	changes := []func(*perspectivePreviewObject){
		func(x *perspectivePreviewObject) { x.Ref.ArtifactID = uuid.Nil },
		func(x *perspectivePreviewObject) { x.Ref.SHA256 = "invalid" },
		func(x *perspectivePreviewObject) { x.Object.VersionID = "null" },
		func(x *perspectivePreviewObject) { x.Object.Bytes = 54525953 },
		func(x *perspectivePreviewObject) { x.Object.ContentType = "application/json" },
		func(x *perspectivePreviewObject) { x.Width = 1601 },
		func(x *perspectivePreviewObject) { x.Height = 0 },
	}
	for i, change := range changes {
		bad := v
		change(&bad)
		if validatePerspectivePreviewObject(bad, "photos", "photos", "preview1600") == nil {
			t.Fatal("invalid locator accepted", i)
		}
	}
	if validatePerspectivePreviewObject(v, "foreign", "photos", "preview1600") == nil {
		t.Fatal("foreign bucket accepted")
	}
	if validatePerspectivePreviewObject(v, "photos", "photos", "export") == nil {
		t.Fatal("unsupported variant accepted")
	}
}
func TestPerspectivePreviewUnconfiguredRefuses(t *testing.T) {
	if _, err := (PerspectiveProposalService{}).Preview(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), uuid.New(), "preview1600"); err == nil {
		t.Fatal("missing signer accepted")
	}
}
func TestPerspectivePreviewSignedURLBindsExactObjectVersion(t *testing.T) {
	for _, raw := range []string{
		"https://objects.example/preview",
		"https://objects.example/preview?versionId=other",
		"https://objects.example/preview?versionId=immutable&versionId=other",
		"https://user@objects.example/preview?versionId=immutable",
	} {
		if validVersionedPreviewURL(raw, "immutable") {
			t.Fatalf("accepted unbound perspective preview URL: %s", raw)
		}
	}
	if !validVersionedPreviewURL("https://objects.example/preview?versionId=immutable&X-Amz-Signature=abc", "immutable") {
		t.Fatal("rejected exact perspective preview version")
	}
}
