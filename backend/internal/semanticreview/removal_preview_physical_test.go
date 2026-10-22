package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPhysicalRemovalPreviewRequiresAuthorityBeforeIO(t *testing.T) {
	got, err := ReadPhysicalRemovalPreview(context.Background(), nil, nil, "", auth.Identity{}, uuid.New(), RemovalPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "remove_object", ModelID: "big_lama", ContextPixels: 64, FeatherPixels: 8}, []byte("{}"), "", "")
	if err == nil || got.verified {
		t.Fatal("unverified preview escaped")
	}
}
