package portal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

func TestExchangeValidation(t *testing.T) {
	valid := ExchangeRequest{ShareID: uuid.New(), Secret: "01234567890123456789012345678901", Locale: " BG "}
	normalized, err := normalizeExchange(valid)
	if err != nil || normalized.Locale != "bg" {
		t.Fatal("valid exchange rejected", normalized, err)
	}
	for _, request := range []ExchangeRequest{
		{ShareID: uuid.Nil, Secret: valid.Secret, Locale: "en"},
		{ShareID: valid.ShareID, Secret: "short", Locale: "en"},
		{ShareID: valid.ShareID, Secret: valid.Secret, Locale: "fr"},
	} {
		if _, err = normalizeExchange(request); err == nil {
			t.Fatal("invalid exchange accepted", request)
		}
	}
}

type portalMultipartFake struct{}

func (portalMultipartFake) BeginMultipart(context.Context, string, string) (string, error) {
	return "multipart", nil
}
func (portalMultipartFake) AbortMultipart(context.Context, string, string) error { return nil }
func (portalMultipartFake) SignUploadParts(_ context.Context, _ string, _ string, parts []int, _ time.Duration) ([]objectstore.PartURL, error) {
	return []objectstore.PartURL{{PartNumber: parts[0], URL: "https://storage.test/part"}}, nil
}
func (portalMultipartFake) SignRead(_ context.Context, object objectstore.Object, _ time.Duration) (objectstore.ReadURL, error) {
	return objectstore.ReadURL{URL: "https://storage.test/read?versionId=" + object.VersionID, ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (portalMultipartFake) SignDownload(context.Context, objectstore.Object, string, time.Duration) (objectstore.ReadURL, error) {
	return objectstore.ReadURL{URL: "https://storage.test/download", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (portalMultipartFake) Check(context.Context, objectstore.Object) error { return nil }

func TestGuestUploadValidationAndRedaction(t *testing.T) {
	profile, hash := "sRGB", strings.Repeat("A", 64)
	request, media, err := normalizeGuestUpload(GuestUploadBegin{Filename: " photo.JPG ", Bytes: 42, Format: "JPEG", AssumeProfile: &profile, SHA256: &hash})
	if err != nil || request.Filename != "photo.JPG" || request.Format != "jpeg" || media != "image/jpeg" || *request.AssumeProfile != "srgb_v1" || *request.SHA256 != strings.ToLower(hash) {
		t.Fatal("valid guest upload rejected", request, media, err)
	}
	for _, invalid := range []GuestUploadBegin{
		{Filename: "photo.png", Bytes: 1, Format: "jpeg"},
		{Filename: "../photo.jpg", Bytes: 1, Format: "jpeg"},
		{Filename: "photo.jpg", Bytes: objectstore.MaxUploadBytes + 1, Format: "jpeg"},
	} {
		if _, _, err = normalizeGuestUpload(invalid); err == nil {
			t.Fatal("invalid guest upload accepted", invalid)
		}
	}
	value, err := json.Marshal(GuestUpload{ID: uuid.New(), State: "uploading", objectKey: "secret/key", multipartID: stringPointer("secret-multipart"), mediaType: "image/jpeg"})
	if err != nil || strings.Contains(string(value), "secret") {
		t.Fatal("guest upload leaks storage identity", string(value), err)
	}
}

func stringPointer(value string) *string { return &value }

func TestGuestCompletionRequiresExactOrderedParts(t *testing.T) {
	upload := GuestUpload{State: "uploading", ExpectedSize: objectstore.PartBytes + 1, PartsTotal: 2, multipartID: stringPointer("multipart")}
	valid := GuestComplete{ExpectedSize: upload.ExpectedSize, Parts: []objectstore.UploadedPart{{PartNumber: 1, ETag: "one"}, {PartNumber: 2, ETag: "two"}}}
	if !validGuestCompletion(valid, upload) {
		t.Fatal("valid completion rejected")
	}
	invalid := valid
	invalid.Parts = []objectstore.UploadedPart{{PartNumber: 2, ETag: "two"}, {PartNumber: 1, ETag: "one"}}
	if validGuestCompletion(invalid, upload) {
		t.Fatal("out-of-order completion accepted")
	}
}

func TestGuestItemProjectionDoesNotSerializeOrderingOrAssetIdentity(t *testing.T) {
	item := GuestItem{ID: uuid.New(), Width: 100, Height: 80, Title: "", ThumbnailStatus: "pending", MediaVariants: json.RawMessage(`[]`), position: 4096}
	encoded, err := json.Marshal(item)
	if err != nil || strings.Contains(string(encoded), "position") || strings.Contains(string(encoded), "asset") || strings.Contains(string(encoded), "artifact") {
		t.Fatal("guest item leaked internal identity", string(encoded), err)
	}
}

func TestMediaRequestValidation(t *testing.T) {
	id := uuid.New()
	request, err := normalizeMediaRequest(MediaRequest{ItemIDs: []uuid.UUID{id}, Variants: []string{" THUMB640 ", "preview1600"}})
	if err != nil || request.Variants[0] != "thumb640" {
		t.Fatal("valid media request rejected", request, err)
	}
	for _, invalid := range []MediaRequest{
		{},
		{ItemIDs: []uuid.UUID{id, id}, Variants: []string{"thumb640"}},
		{ItemIDs: []uuid.UUID{id}, Variants: []string{"master"}},
		{ItemIDs: []uuid.UUID{id}, Variants: []string{"thumb640", "thumb640"}},
	} {
		if _, err = normalizeMediaRequest(invalid); err == nil {
			t.Fatal("invalid media request accepted", invalid)
		}
	}
}

func TestPortalMediaLinkRequiresExactVersionAndBoundedExpiry(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	object := objectstore.Object{VersionID: "v1"}
	link := objectstore.ReadURL{URL: "https://storage.test/read?versionId=v1", ExpiresAt: now.Add(time.Minute)}
	if !validPortalMediaReadURL(object, link, now) {
		t.Fatal("valid portal media link refused")
	}
	for _, raw := range []string{"http://storage.test/read?versionId=v1", "https://storage.test/read?versionId=other", "https://storage.test/read?versionId=v1&versionId=other"} {
		link.URL = raw
		if validPortalMediaReadURL(object, link, now) {
			t.Fatal("unsafe portal media link accepted", raw)
		}
	}
	link.URL = "http://localhost:9000/read?versionId=v1"
	if !validPortalMediaReadURL(object, link, now) {
		t.Fatal("local portal media link refused")
	}
	link.ExpiresAt = now.Add(time.Minute + 6*time.Second)
	if validPortalMediaReadURL(object, link, now) {
		t.Fatal("overlong portal media link accepted")
	}
}

func TestSelectionNormalization(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	request, revision, section, err := normalizeSelection(SelectionRequest{CollectionRevision: "7", Scope: " SELECTED ", ItemIDs: []uuid.UUID{b, a}})
	if err != nil || revision != 7 || section != nil || request.Scope != "selected" || request.ItemIDs[0].String() > request.ItemIDs[1].String() {
		t.Fatal("valid selection rejected", request, revision, section, err)
	}
	sectionID := uuid.New()
	if _, _, selected, err := normalizeSelection(SelectionRequest{CollectionRevision: "8", Scope: "filter", Filter: &SelectionFilter{SectionID: sectionID}}); err != nil || selected == nil || *selected != sectionID {
		t.Fatal("valid filter rejected", selected, err)
	}
	for _, invalid := range []SelectionRequest{
		{CollectionRevision: "0", Scope: "all"},
		{CollectionRevision: "1", Scope: "selected"},
		{CollectionRevision: "1", Scope: "selected", ItemIDs: []uuid.UUID{a, a}},
		{CollectionRevision: "1", Scope: "all", ItemIDs: []uuid.UUID{a}},
		{CollectionRevision: "1", Scope: "filter", Filter: &SelectionFilter{}},
	} {
		if _, _, _, err = normalizeSelection(invalid); err == nil {
			t.Fatal("invalid selection accepted", invalid)
		}
	}
}

type downloadProjectionRow struct {
	withOutcome bool
	outcome     string
	filesDone   int64
	filesTotal  int64
}

func (r downloadProjectionRow) Scan(dest ...any) error {
	offset := 0
	if r.withOutcome {
		if r.outcome == "" {
			r.outcome = "ok"
		}
		*dest[0].(*string) = r.outcome
		offset = 1
		if r.outcome != "ok" {
			return nil
		}
	}
	id, status, written := uuid.New(), "building", int64(12)
	done, total := r.filesDone, r.filesTotal
	*dest[offset].(**uuid.UUID) = &id
	*dest[offset+1].(**string) = &status
	*dest[offset+2].(**int64) = &done
	*dest[offset+3].(**int64) = &total
	*dest[offset+4].(**int64) = &written
	*dest[offset+5].(*[]byte) = []byte(`[{"number":1,"status":"building","bytes":12,"delivery_kind":"zip"}]`)
	*dest[offset+6].(**time.Time) = nil
	*dest[offset+7].(*[]byte) = nil
	return nil
}

func TestDownloadProjectionValidationAndRedaction(t *testing.T) {
	out, outcome, err := scanDownload(downloadProjectionRow{withOutcome: true, filesDone: 1, filesTotal: 2}, true)
	if err != nil || outcome != "ok" || len(out.Volumes) != 1 || out.Volumes[0].Number != 1 {
		t.Fatal("valid download projection rejected", out, outcome, err)
	}
	encoded, err := json.Marshal(out)
	if err != nil || strings.Contains(string(encoded), "artifact") || strings.Contains(string(encoded), "task") || strings.Contains(string(encoded), "storage") {
		t.Fatal("download status leaked internal identity", string(encoded), err)
	}
	if _, _, err = scanDownload(downloadProjectionRow{filesDone: 3, filesTotal: 2}, false); err == nil {
		t.Fatal("invalid download counters accepted")
	}
	if empty, outcome, err := scanDownload(downloadProjectionRow{withOutcome: true, outcome: "unauthorized"}, true); err != nil || outcome != "unauthorized" || empty.ID != uuid.Nil {
		t.Fatal("nullable failure outcome rejected", empty, outcome, err)
	}
}

func TestSessionInfoDoesNotSerializeCookieToken(t *testing.T) {
	encoded, err := json.Marshal(SessionInfo{ID: uuid.New(), Token: "secret-cookie-token", CSRFToken: "public-csrf"})
	if err != nil || strings.Contains(string(encoded), "secret-cookie-token") || !strings.Contains(string(encoded), "public-csrf") {
		t.Fatal("portal session serialization leaked cookie token", string(encoded), err)
	}
}
