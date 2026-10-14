package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

func TestMultiPreviewOutputsExactPair(t *testing.T) {
	run := uuid.New()
	task := uuid.NewSHA1(run, []byte("render.derivative:preview1600"))
	result := uuid.New()
	image := MultiPreviewOutput{Task: task, Result: result, Artifact: uuid.New(), Attempt: 1, Fence: 2, Params: "{}", Role: "derivative", Bucket: "photos", Object: objectstore.Object{Key: "image", VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 20, ContentType: "image/jpeg"}}
	manifest := image
	manifest.Artifact = uuid.New()
	manifest.Role = "derivative_manifest"
	manifest.Object.Key = "manifest"
	manifest.Object.ContentType = "application/json"
	pair := []MultiPreviewOutput{image, manifest}
	if err := validateMultiPreviewOutputs(run, "preview1600", "photos", pair); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"fence", "result", "version", "role", "artifact", "bucket", "mime", "params"} {
		bad := append([]MultiPreviewOutput(nil), pair...)
		switch kind {
		case "fence":
			bad[1].Fence++
		case "result":
			bad[1].Result = uuid.New()
		case "version":
			bad[1].Object.VersionID = "null"
		case "role":
			bad[1].Role = "derivative"
		case "artifact":
			bad[1].Artifact = bad[0].Artifact
		case "bucket":
			bad[1].Bucket = "other"
		case "mime":
			bad[1].Object.ContentType = "image/jpeg"
		case "params":
			bad[1].Params = "{\"x\":1}"
		}
		if err := validateMultiPreviewOutputs(run, "preview1600", "photos", bad); err == nil {
			t.Fatal("substitution accepted", kind)
		}
	}
	if err := validateMultiPreviewOutputs(run, "preview1600", "photos", pair[:1]); err == nil {
		t.Fatal("partial pair accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (MultiPreviewResults{}).Read(ctx, uuid.New(), run, "preview1600"); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := (MultiPreviewResults{}).Read(context.Background(), uuid.New(), run, "preview1600"); err == nil {
		t.Fatal("missing DB accepted")
	}
}
