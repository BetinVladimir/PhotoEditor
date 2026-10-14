package v2learning

import "testing"

func TestSeriesObjectReaderAllowsOnlyFrozenJSONRoles(t *testing.T) {
	for _, role := range []string{"source_image", "stage_image", "params", "series"} {
		if !validSeriesObjectRole(role) {
			t.Fatalf("frozen role %q refused", role)
		}
	}
	for _, role := range []string{"", "stats", "mask", "other", "SeriesCorrections"} {
		if validSeriesObjectRole(role) {
			t.Fatalf("unregistered role %q admitted", role)
		}
	}
}
