package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRetouchPresentationLinkageCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := ReadRetouchPreviewPresentationLinkage(ctx, nil, nil, "", auth.Identity{}, uuid.New(), RetouchPreviewParams{}, v2input.Ref{}, "", "")
	if err != context.Canceled || got.Document.TaskID != uuid.Nil {
		t.Fatal("cancelled linkage escaped", err)
	}
}

func TestRetouchPresentationLinkageMissingAuthorityBeforeIO(t *testing.T) {
	pin := strings.Repeat("a", 64)
	got, err := ReadRetouchPreviewPresentationLinkage(context.Background(), nil, nil, "", auth.Identity{}, uuid.New(), RetouchPreviewParams{}, v2input.Ref{}, pin, pin)
	if err == nil || got.Document.TaskID != uuid.Nil {
		t.Fatal("unauthorized linkage escaped")
	}
}

func TestRetouchPresentationLinkageMissingDisplayPinsBeforeIO(t *testing.T) {
	got, err := ReadRetouchPreviewPresentationLinkage(context.Background(), nil, nil, "", auth.Identity{}, uuid.New(), RetouchPreviewParams{}, v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, "", "")
	if err != ErrInvalid || got.Document.TaskID != uuid.Nil {
		t.Fatal("unpinned linkage escaped", err)
	}
}

func TestRetouchSidecarRoleBudgets(t *testing.T) {
	limits := []int64{1048576,1048576,2400016384,33882112,33882112,32833536,32833536}
	media := []string{"application/json","application/json","application/x-npy","image/png","image/png","image/png","image/png"}
	for role := range limits {
		for _, size := range []int64{0,1,2,limits[role],limits[role]+1} {
			source := PlanSource{Bytes:size,MediaType:media[role]}
			want := size >= 2 && size <= limits[role]
			if validRetouchPreviewSidecar(role,source) != want { t.Fatal("wrong byte boundary",role,size) }
			for _, wrong := range []string{"application/octet-stream","text/plain","", "application/json", "application/x-npy", "image/png"} {
				if wrong == media[role] { continue }
				source.MediaType = wrong
				if validRetouchPreviewSidecar(role,source) { t.Fatal("wrong media accepted",role,wrong) }
			}
		}
	}
	for _, role := range []int{-1,7,100} {
		if validRetouchPreviewSidecar(role,PlanSource{Bytes:2,MediaType:"application/json"}) { t.Fatal("unknown role accepted",role) }
	}
}
