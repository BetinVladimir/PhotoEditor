package v2draft

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestRestorationArrayLocatorRequiresExactVersionAndSchema(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	value := map[string]any{"key": "tenants/pinned.npy", "version": "v1",
		"sha256": ref.SHA256, "bytes": int64(100), "media_type": "application/x-npy"}
	encode := func() []byte {
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if object, err := parseRestorationArrayLocator(encode(), ref); err != nil ||
		object.VersionID != "v1" || object.SHA256 != ref.SHA256 {
		t.Fatal("valid locator refused", object, err)
	}
	for name, change := range map[string]func(){
		"null version": func() { value["version"] = "null" },
		"wrong digest": func() { value["sha256"] = strings.Repeat("b", 64) },
		"wrong media":  func() { value["media_type"] = "application/json" },
		"extra field":  func() { value["bucket"] = "foreign" },
	} {
		original := map[string]any{}
		for key, item := range value {
			original[key] = item
		}
		change()
		if _, err := parseRestorationArrayLocator(encode(), ref); err == nil {
			t.Fatal("invalid locator accepted", name)
		}
		value = original
	}
	if restorationArrayRole("task_params") || !restorationArrayRole("changed_pixels") {
		t.Fatal("array role allowlist changed")
	}
}
