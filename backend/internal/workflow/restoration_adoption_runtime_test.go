package workflow

import "testing"

func TestRestorationAdoptionConfigurationFailsClosed(t *testing.T) {
	values := map[string]string{}
	getenv := func(key string) string { return values[key] }
	if _, enabled, err := RestorationAdoptionFromEnvironment(getenv); err != nil || enabled {
		t.Fatal("absent installation enabled", err)
	}
	values["PHOTO_V28_RESTORATION_ADOPTION_ENABLED"] = "true"
	if _, enabled, err := RestorationAdoptionFromEnvironment(getenv); err == nil || enabled {
		t.Fatal("catalog authority missing but enabled")
	}
	values["PHOTO_V28_RESTORATION_CATALOG_DATABASE_URL"] = "postgres://example.invalid/photo"
	config, enabled, err := RestorationAdoptionFromEnvironment(getenv)
	if err != nil || !enabled || config.CatalogDatabaseURL != values["PHOTO_V28_RESTORATION_CATALOG_DATABASE_URL"] {
		t.Fatal("explicit installation refused", err)
	}
	values["PHOTO_V28_RESTORATION_ADOPTION_ENABLED"] = "yes"
	if _, enabled, err := RestorationAdoptionFromEnvironment(getenv); err == nil || enabled {
		t.Fatal("ambiguous enabled flag accepted")
	}
}
