package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPerspectiveResultGeometryRefusesMissingOriginal(t *testing.T) {
	if err := verifyPerspectiveResultGeometry(AdoptedPerspectiveBinding{}, v2draft.ImageResultDocument{}, []byte(`{}`), nil); err == nil {
		t.Fatal("missing source accepted")
	}
	if _, err := readPerspectiveGeometrySource(context.Background(), nil, nil, auth.Identity{}, uuid.New(), v2input.Ref{}); err == nil {
		t.Fatal("missing original authority accepted")
	}
}
