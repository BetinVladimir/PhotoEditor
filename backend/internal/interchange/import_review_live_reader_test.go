package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestImportLiveReviewReaderRequiresScopedInput(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("base"))}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref, CanvasHash: rawSHA([]byte("canvas"))}
	_, err := (SQLImportLiveReviewReader{}).Read(context.Background(), uuid.New(), binding, "dust", v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("proposal"))}, ref, nil)
	if err == nil {
		t.Fatal("uninstalled live reader accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (SQLImportLiveReviewReader{}).Read(ctx, uuid.New(), binding, "dust", ref, ref, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled live reader: %v", err)
	}
}

func TestImportLiveReviewIDsRejectDuplicatesAndUnsorted(t *testing.T) {
	a, b := rawSHA([]byte("a")), rawSHA([]byte("b"))
	if !validLiveReviewIDs([]string{a, b}, nil) && !validLiveReviewIDs([]string{b, a}, nil) {
		t.Fatal("no valid ordering")
	}
	if validLiveReviewIDs([]string{a, a}, nil) || validLiveReviewIDs([]string{a}, []string{a}) || validLiveReviewIDs([]string{"bad"}, nil) {
		t.Fatal("invalid review IDs accepted")
	}
}
