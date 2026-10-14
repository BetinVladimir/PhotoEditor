package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportPersonOriginInputsFailClosed(t *testing.T) {
	reader := SchedulerExportPersonOriginInputs{Bucket: "photos"}
	if _, err := reader.Read(context.Background(), ExportAdoptionClaim{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled reader: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Read(ctx, ExportAdoptionClaim{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled reader: %v", err)
	}
}

func TestSchedulerExportPersonOriginInputPin(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	input := exportPersonOriginStoredInput{ref: ref, object: objectstore.Object{
		Key: "tenants/example/document", VersionID: "version", Bytes: 128,
		ContentType: "application/json", SHA256: ref.SHA256,
	}}
	if err := validSchedulerPersonOriginInput(input, "photos", "photos"); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*exportPersonOriginStoredInput){
		"unversioned": func(i *exportPersonOriginStoredInput) { i.object.VersionID = "null" },
		"wrong media": func(i *exportPersonOriginStoredInput) { i.object.ContentType = "text/plain" },
		"wrong hash": func(i *exportPersonOriginStoredInput) {
			i.object.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		},
		"oversize": func(i *exportPersonOriginStoredInput) { i.object.Bytes = 16385 },
	} {
		changed := input
		mutate(&changed)
		if err := validSchedulerPersonOriginInput(changed, "photos", "photos"); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if err := validSchedulerPersonOriginInput(input, "other", "photos"); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("foreign bucket accepted: %v", err)
	}
}
