package workflow

import (
	"context"
	"testing"
)

func TestRestorationPreviewRuntimeRequiresExplicitInstallation(t *testing.T) {
	values := map[string]string{}
	getenv := func(key string) string { return values[key] }
	if enabled, err := RestorationPreviewFromEnvironment(getenv); err != nil || enabled {
		t.Fatal("absent preview enabled", err)
	}
	values["PHOTO_V28_RESTORATION_PREVIEW_ENABLED"] = "true"
	if enabled, err := RestorationPreviewFromEnvironment(getenv); err != nil || !enabled {
		t.Fatal("explicit preview disabled", err)
	}
	values["PHOTO_V28_RESTORATION_PREVIEW_ENABLED"] = "yes"
	if enabled, err := RestorationPreviewFromEnvironment(getenv); err == nil || enabled {
		t.Fatal("ambiguous flag accepted")
	}
	if _, err := NewRestorationPreviewProcessor(context.Background(), nil, nil,
		"photos", "", ""); err == nil {
		t.Fatal("uninstalled processor accepted")
	}
}
