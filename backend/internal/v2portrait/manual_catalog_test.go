package v2portrait

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestManualPairObjectsRejectInvalidContextBeforeDatabase(t *testing.T) {
	ref := v2input.Ref{}
	for _, bucket := range []string{"", "photos"} {
		got, err := ReadManualPairObjects(context.Background(), nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, v2input.SessionVersionBinding{}, ref, ref, ref, ref, bucket)
		if err == nil || got != (ManualPairObjects{}) {
			t.Fatal("invalid catalog request accepted", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := ReadManualPairObjects(ctx, nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, v2input.SessionVersionBinding{}, ref, ref, ref, ref, "photos")
	if !errors.Is(err, context.Canceled) || got != (ManualPairObjects{}) {
		t.Fatal("cancellation lost", got, err)
	}
	origin, err := ReadManualPairOrigin(ctx, nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, v2input.SessionVersionBinding{}, ref, ref, ref, ref)
	if !errors.Is(err, context.Canceled) || origin != (ManualPairOrigin{}) {
		t.Fatal("origin cancellation lost", origin, err)
	}
}

func TestManualVectorsObjectRejectsInvalidContextBeforeDatabase(t *testing.T) {
	ref := v2input.Ref{}
	got, err := ReadManualVectorsObject(context.Background(), nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, v2input.SessionVersionBinding{}, ref, ref, ref, ref, ManualPairOrigin{}, ref, "photos")
	if err == nil || got.Key != "" {
		t.Fatal("invalid vectors request accepted", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = ReadManualVectorsObject(ctx, nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, v2input.SessionVersionBinding{}, ref, ref, ref, ref, ManualPairOrigin{}, ref, "photos")
	if !errors.Is(err, context.Canceled) || got.Key != "" {
		t.Fatal("vectors cancellation lost", got, err)
	}
}

func TestManualPairDocumentsCancelAndMissingReader(t *testing.T) {
	ref := v2input.Ref{}
	got, err := ReadManualPairDocuments(context.Background(), nil, nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, v2input.SessionVersionBinding{}, []byte("{}"), ref, ref, ref, ref, "photos")
	if err == nil || got.RootRaw != nil || got.ParamsRaw != nil || got.VectorsRaw != nil {
		t.Fatal("missing reader accepted", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = ReadManualPairDocuments(ctx, nil, nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, v2input.SessionVersionBinding{}, nil, ref, ref, ref, ref, "photos")
	if !errors.Is(err, context.Canceled) || got.RootRaw != nil || got.ParamsRaw != nil || got.VectorsRaw != nil {
		t.Fatal("document cancellation lost", got, err)
	}
}
