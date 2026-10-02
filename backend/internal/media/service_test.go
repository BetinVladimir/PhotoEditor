package media

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestProfileHashIsPolicyBound(t *testing.T) {
	a, err := ProfileHash("thumb320", strings.Repeat("a", 64))
	if err != nil || len(a) != 64 {
		t.Fatal(a, err)
	}
	if a != "ebe90336177eb9b7dd29dd048d5a4ede9fcfaa9e87ede89e1391c7ea57f259f4" {
		t.Fatal("profile hash differs from worker contract", a)
	}
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
