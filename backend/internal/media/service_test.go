package media

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
	"time"
)

func TestMediaSignedReadRequiresExactVersionAndSafeTransport(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	object := objectstore.Object{VersionID: "immutable-v1"}
	link := objectstore.ReadURL{URL: "https://objects.example/thumb?versionId=immutable-v1", ExpiresAt: now.Add(10 * time.Minute)}
	if !validMediaSignedRead(object, link, now) {
		t.Fatal("valid signed media link refused")
	}
	for _, raw := range []string{"http://objects.example/thumb?versionId=immutable-v1", "https://objects.example/thumb?versionId=other", "https://objects.example/thumb?versionId=immutable-v1&versionId=other", "https://objects.example/thumb?versionId=immutable-v1#fragment"} {
		link.URL = raw
		if validMediaSignedRead(object, link, now) {
			t.Fatal("unsafe signed media link accepted", raw)
		}
	}
	link.URL = "http://127.0.0.1:9000/thumb?versionId=immutable-v1"
	if !validMediaSignedRead(object, link, now) {
		t.Fatal("local signed media link refused")
	}
	link.ExpiresAt = now.Add(10*time.Minute + 6*time.Second)
	if validMediaSignedRead(object, link, now) {
		t.Fatal("overlong signed media link accepted")
	}
}

func TestProfileHashIsPolicyBound(t *testing.T) {
	a, err := ProfileHash("thumb320", strings.Repeat("a", 64))
	if err != nil || len(a) != 64 {
		t.Fatal(a, err)
	}
	wire:=`{"icc_sha256":"`+strings.Repeat("a",64)+`","long_edge":320,"metadata_policy":"stripped","no_upscale":true,"quality":82,"renderer_policy":"declared_encoding_alpha8_icc_v2","schema_version":1,"variant":"thumb320"}`
	digest:=sha256.Sum256([]byte(wire))
	if a != hex.EncodeToString(digest[:]) {
		t.Fatal("profile hash differs from worker contract", a)
	}
	legacy,_:=LegacyProfileHash("thumb320",strings.Repeat("a",64))
	if legacy!="ebe90336177eb9b7dd29dd048d5a4ede9fcfaa9e87ede89e1391c7ea57f259f4" || legacy==a {t.Fatal("legacy/current media policies collided")}
	b, _ := ProfileHash("thumb640", strings.Repeat("a", 64))
	if a == b {
		t.Fatal("variant omitted from profile hash")
	}
	if _, err = ProfileHash("master", strings.Repeat("a", 64)); err == nil {
		t.Fatal("unknown variant accepted")
	}
}

func TestNormalizeRejectsDuplicateAndUnknownVariants(t *testing.T) {
	sha := strings.Repeat("a", 64)
	version := uuid.New()
	request, hashes, err := normalize(Request{AssetVersionID: version, Variants: []string{"thumb320", "preview1600"}}, sha)
	if err != nil || request.AssetVersionID != version || len(hashes) != 2 {
		t.Fatalf("%+v %+v %v", request, hashes, err)
	}
	for _, variants := range [][]string{{}, {"thumb320", "thumb320"}, {"master"}} {
		if _, _, err = normalize(Request{AssetVersionID: version, Variants: variants}, sha); err == nil {
			t.Fatal("invalid variants accepted", variants)
		}
	}
}
