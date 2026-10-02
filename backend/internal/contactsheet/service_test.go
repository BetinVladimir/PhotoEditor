package contactsheet

import (
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/domain"
	"testing"
)

func TestBundledFontIsPinned(t *testing.T) {
	raw, sha, err := fontBytes()
	if err != nil || len(raw) != 569208 || sha != "b85c38ecea8a7cfb39c24e395a4007474fa5a4fc864f6ee33309eb4948d232d5" {
		t.Fatalf("font drift: bytes=%d sha=%s err=%v", len(raw), sha, err)
	}
}
func TestNormalizeCreateRequest(t *testing.T) {
	id := uuid.New()
	got, err := normalize(CreateRequest{SelectionSnapshotID: id, Locale: " BG ", Columns: 4, Rows: 5})
	if err != nil || got.Locale != "bg" {
		t.Fatalf("normalize: %#v %v", got, err)
	}
	for _, bad := range []CreateRequest{{SelectionSnapshotID: id, Locale: "de", Columns: 4, Rows: 5}, {SelectionSnapshotID: id, Locale: "en", Columns: 0, Rows: 5}, {SelectionSnapshotID: id, Locale: "en", Columns: 4, Rows: 11}} {
		if _, err = normalize(bad); err == nil {
			t.Fatalf("accepted %#v", bad)
		}
	}
}
func TestSafeLabelIsNonemptyAndBounded(t *testing.T) {
	if got := safeLabel(" ", 8); got != "0009" {
		t.Fatal(got)
	}
	long := make([]rune, 200)
	for i := range long {
		long[i] = 'я'
	}
	if got := safeLabel(string(long), 0); len([]rune(got)) != 160 {
		t.Fatal(len([]rune(got)))
	}
}

func TestResourceErrorsHaveStableAPIClassification(t *testing.T) {
	tests := []struct {
		err    error
		code   string
		status int
	}{
		{artifactjson.ErrQuota, "QUOTA_EXCEEDED", 422},
		{artifactjson.ErrConflict, "CONTACT_SHEET_RESOURCE_CONFLICT", 409},
		{artifactjson.ErrStale, "CONTACT_SHEET_RESOURCE_STALE", 409},
		{errors.New("store offline"), "DEPENDENCY_UNAVAILABLE", 503},
	}
	for _, tc := range tests {
		got := domain.AsError(resourceError(tc.err))
		if got.Code != tc.code || got.HTTPStatus != tc.status {
			t.Fatalf("%v: got %s/%d", tc.err, got.Code, got.HTTPStatus)
		}
	}
}
