package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

func TestRestorationPreviewOutputPairBindsSideAndVersion(t *testing.T) {
	run := uuid.New()
	task := restorationPreviewTaskID(run, "after", "preview2560")
	first := MultiPreviewOutput{Task: task, Result: uuid.New(), Artifact: uuid.New(),
		Attempt: 1, Fence: 2, Params: "{}", Role: "derivative", Bucket: "photos",
		Object: objectstore.Object{Key: "image", VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 20, ContentType: "image/jpeg"}}
	second := first
	second.Artifact = uuid.New()
	second.Role = "derivative_manifest"
	second.Object = objectstore.Object{Key: "manifest", VersionID: "v2", SHA256: strings.Repeat("b", 64), Bytes: 30, ContentType: "application/json"}
	if err := validateRestorationPreviewOutputs(run, "after", "preview2560", "photos", []MultiPreviewOutput{first, second}); err != nil {
		t.Fatal(err)
	}
	if err := validateRestorationPreviewOutputs(run, "before", "preview2560", "photos", []MultiPreviewOutput{first, second}); !errors.Is(err, objectstore.ErrIntegrity) {
		t.Fatal("cross-side outputs accepted", err)
	}
	if err := validateRestorationPreviewOutputs(run, "after", "preview1600", "photos", []MultiPreviewOutput{first, second}); !errors.Is(err, objectstore.ErrIntegrity) {
		t.Fatal("cross-variant outputs accepted", err)
	}
	second.Object.VersionID = "null"
	if err := validateRestorationPreviewOutputs(run, "after", "preview2560", "photos", []MultiPreviewOutput{first, second}); !errors.Is(err, objectstore.ErrIntegrity) {
		t.Fatal("unversioned manifest accepted", err)
	}
}

func TestRestorationPreviewResultReaderRefusesInvalidCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (RestorationPreviewResults{}).Read(ctx, uuid.New(), uuid.New(), "before", "preview1600"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := (RestorationPreviewResults{}).Read(context.Background(), uuid.New(), uuid.New(), "unknown", "preview1600"); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal(err)
	}
}
