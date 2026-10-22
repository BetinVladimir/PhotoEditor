package styleupload

import (
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

func TestStylePackageBoundsAndKinds(t *testing.T) {
	for _, name := range []string{"style.json", "original.ZIP"} {
		for _, size := range []int64{2, 16777216, 16777217, 104857600} {
			if _, _, e := normalize(BeginRequest{name, size, strings.Repeat("a", 64)}); e != nil {
				t.Fatalf("valid %s %d: %v", name, size, e)
			}
		}
	}
	for _, r := range []BeginRequest{{"photo.jpg", 20, strings.Repeat("a", 64)}, {"../style.zip", 20, strings.Repeat("a", 64)}, {"style.json", 104857601, strings.Repeat("a", 64)}, {"style.zip", 1, strings.Repeat("a", 64)}, {"style.zip", 20, "bad"}, {"style.zip ", 20, strings.Repeat("a", 64)}} {
		if _, _, e := normalize(r); e == nil {
			t.Fatal("invalid package accepted")
		}
	}
}
func TestStylePackageCompletionExactParts(t *testing.T) {
	r := row{Session: Session{Bytes: 16777219, Parts: 2}}
	good := CompleteRequest{[]objectstore.UploadedPart{{PartNumber: 1, ETag: "one"}, {PartNumber: 2, ETag: "last"}}, 16777219}
	if e := validateComplete(r, good); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []CompleteRequest{{good.Parts, 16777218}, {good.Parts[:1], 16777219}, {[]objectstore.UploadedPart{{2, "last"}, {1, "one"}}, 16777219}, {[]objectstore.UploadedPart{{1, "one"}, {1, "dup"}}, 16777219}, {[]objectstore.UploadedPart{{1, "one"}, {2, "bad\n"}}, 16777219}} {
		if validateComplete(r, bad) == nil {
			t.Fatal("malformed completion accepted")
		}
	}
}
func TestStylePackageCompletionIdentityBindsKeyAndBody(t *testing.T) {
	request := CompleteRequest{[]objectstore.UploadedPart{{PartNumber: 1, ETag: "first"}}, 2}
	identity := func(key string, r CompleteRequest) string {
		v, e := canonical.Hash(struct {
			Key  string          `json:"key"`
			Body CompleteRequest `json:"body"`
		}{key, r})
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	old := identity("same", request)
	if identity("same", request) != old {
		t.Fatal("unstable identity")
	}
	if identity("other", request) == old {
		t.Fatal("key unbound")
	}
	request.Parts[0].ETag = "changed"
	if identity("same", request) == old {
		t.Fatal("ETag unbound")
	}
}
