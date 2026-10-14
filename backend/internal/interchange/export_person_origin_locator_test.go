package interchange

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestPortablePersonOriginLocatorRefusesUninstalledAndInvalidScope(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	source := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(),
		CanvasHash: strings.Repeat("b", 64)}
	root, pixels := ref(), ref()
	catalog := SQLExportDocumentCatalog{}
	if _, err := catalog.LookupPortablePersonOrigin(context.Background(), source, root, pixels); err == nil {
		t.Fatal("uninstalled origin locator admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupPortablePersonOrigin(ctx, source, root, pixels); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	source.Source.SHA256 = "foreign"
	if _, err := catalog.LookupPortablePersonOrigin(context.Background(), source, root, pixels); err == nil {
		t.Fatal("invalid original source admitted")
	}
}
