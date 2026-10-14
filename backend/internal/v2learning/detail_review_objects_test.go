package v2learning

import (
	"testing"

	"photoeditor/backend/internal/objectstore"
)

func TestPortraitDetailObjectRolesAndDescriptor(t *testing.T) {
	for _, role := range []string{"image", "params", "resolved"} {
		if !validPortraitDetailObjectRole(role) {
			t.Fatalf("valid role %s refused", role)
		}
	}
	for _, role := range []string{"source", "mask", "protection", "report", ""} {
		if validPortraitDetailObjectRole(role) {
			t.Fatalf("unregistered role %s admitted", role)
		}
	}
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	good := objectstore.Object{Key: "tenant/detail", VersionID: "version",
		SHA256: sha, Bytes: 1024, ContentType: "application/json"}
	if !validPortraitDetailObject(good, "bucket", sha, "bucket") {
		t.Fatal("exact versioned descriptor refused")
	}
	for _, changed := range []objectstore.Object{
		{Key: good.Key, VersionID: "", SHA256: sha,
			Bytes: good.Bytes, ContentType: good.ContentType},
		{Key: good.Key, VersionID: good.VersionID, SHA256: sha,
			Bytes: good.Bytes, ContentType: "application/octet-stream"},
		{Key: good.Key, VersionID: good.VersionID, SHA256: sha,
			Bytes: (1 << 20) + 1, ContentType: good.ContentType},
	} {
		if validPortraitDetailObject(changed, "bucket", sha, "bucket") {
			t.Fatal("invalid descriptor admitted")
		}
	}
	if validPortraitDetailObject(good, "other", sha, "bucket") {
		t.Fatal("foreign bucket admitted")
	}
}
