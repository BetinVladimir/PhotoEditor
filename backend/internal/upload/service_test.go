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
	beginErr              error
	beginID               string
}

func (f *multipartFake) BeginMultipart(context.Context, string, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begins++
	if f.beginErr != nil {
		return "", f.beginErr
	}
	if f.beginID != "" {
		return f.beginID, nil
	}
	return "multipart-token", nil
}
func (f *multipartFake) BeginCaptureMultipart(ctx context.Context, tenant, upload uuid.UUID, mediaType string) (string, error) {
	key, err := objectstore.StagingKey(tenant, upload)
	if err != nil {
		return "", err
	}
	return f.BeginMultipart(ctx, key, mediaType)
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

func TestUploadCompletionETagMatchesStorageAdmission(t *testing.T) {
	session := Session{ExpectedSize: 4, PartsTotal: 1}
	request := CompleteRequest{ExpectedSize: 4, Parts: []objectstore.UploadedPart{{PartNumber: 1, ETag: "etag"}}}
	if !validCompletion(request, session) {
		t.Fatal("valid multipart receipt rejected")
	}
	for _, invalid := range []string{"", "bad\x00etag", "bad\retag", "bad\netag", strings.Repeat("x", 257)} {
		request.Parts[0].ETag = invalid
		if validCompletion(request, session) {
			t.Fatal("invalid ETag admitted to completion queue", invalid)
		}
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
