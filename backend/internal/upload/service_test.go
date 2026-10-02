package upload

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

type multipartFake struct {
	mu                    sync.Mutex
	begins, aborts, signs int
}

func (f *multipartFake) BeginMultipart(context.Context, string, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begins++
	return "multipart-token", nil
}
func (f *multipartFake) AbortMultipart(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aborts++
	return nil
}
func (f *multipartFake) SignUploadParts(_ context.Context, _ string, _ string, parts []int, _ time.Duration) ([]objectstore.PartURL, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signs++
	out := make([]objectstore.PartURL, len(parts))
	for i, part := range parts {
		out[i] = objectstore.PartURL{PartNumber: part, URL: "https://storage.test/part"}
	}
	return out, nil
}

func TestBeginValidationAndSessionRedaction(t *testing.T) {
	project := uuid.New()
	profile := "srgb"
	hash := strings.Repeat("A", 64)
	request, media, err := normalize(BeginRequest{ProjectID: project, Filename: " photo.JPG ", Bytes: 123, Format: "JPEG", AssumeProfile: &profile, SHA256: &hash})
	if err != nil || request.Filename != "photo.JPG" || request.Format != "jpeg" || media != "image/jpeg" || request.SHA256 == nil || *request.SHA256 != strings.ToLower(hash) {
		t.Fatal("valid upload rejected", request, media, err)
	}
	for _, invalid := range []BeginRequest{
		{ProjectID: project, Filename: "photo.png", Bytes: 1, Format: "jpeg"},
		{ProjectID: project, Filename: "../photo.jpg", Bytes: 1, Format: "jpeg"},
		{ProjectID: project, Filename: "photo.jpg", Bytes: objectstore.MaxUploadBytes + 1, Format: "jpeg"},
		{ProjectID: project, Filename: "photo.jpg", Bytes: 1, Format: "exe"},
	} {
		if _, _, err = normalize(invalid); err == nil {
			t.Fatal("invalid upload accepted", invalid)
		}
	}
	session := Session{ID: uuid.New(), ProjectID: project, State: "uploading", objectKey: "secret/key", multipartID: ptr("secret-token"), mediaType: "image/jpeg"}
	encoded, err := json.Marshal(session)
	if err != nil || strings.Contains(string(encoded), "secret") || !strings.Contains(string(encoded), "uploading") {
		t.Fatal("session leaks internal storage identity", string(encoded), err)
	}
}

func TestModerationValidation(t *testing.T) {
	request, err := normalizeModeration(ModerationRequest{Decision: " ACCEPT ", ExpectedRevision: 7})
	if err != nil || request.Decision != "accept" {
		t.Fatal("valid moderation rejected", request, err)
	}
	for _, invalid := range []ModerationRequest{
		{Decision: "", ExpectedRevision: 1},
		{Decision: "publish", ExpectedRevision: 1},
		{Decision: "reject", ExpectedRevision: 0},
		{Decision: "accept", ExpectedRevision: 9007199254740992},
	} {
		if _, err = normalizeModeration(invalid); err == nil {
			t.Fatal("invalid moderation accepted", invalid)
		}
	}
}

func ptr(value string) *string { return &value }
