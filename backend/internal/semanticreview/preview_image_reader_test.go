package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPreviewImageReaderRejectsInvalidAndCancelledBeforeIO(t *testing.T) {
	p := previewParamsFixture()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if got, err := ReadExposurePreviewImage(context.Background(), nil, nil, "fixture", who, uuid.New(), p, nil); err == nil || got.Evidence.TaskID != uuid.Nil {
		t.Fatal("empty source accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := ReadExposurePreviewImage(ctx, nil, nil, "fixture", who, uuid.New(), p, []byte("{}")); err != context.Canceled || got.Evidence.TaskID != uuid.Nil {
		t.Fatal("cancel ignored", err)
	}
}
