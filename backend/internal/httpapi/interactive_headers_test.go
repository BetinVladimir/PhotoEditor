package httpapi

import (
	"net/http"
	"testing"
	"photoeditor/backend/internal/domain"
)

func TestGenerationReplayModeCannotBeAmbiguous(t *testing.T) {
	for _, tc := range []struct{name string; values []string; replay,invalid bool}{
		{"ordinary", nil, false, false},
		{"lookup only", []string{"true"}, true, false},
		{"explicit empty", []string{""}, false, true},
		{"false is not a mode", []string{"false"}, false, true},
		{"comma folded", []string{"true,false"}, false, true},
		{"duplicate equal", []string{"true","true"}, false, true},
		{"duplicate conflicting", []string{"true","false"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := make(http.Header)
			for _, value := range tc.values { headers.Add("Generation-Replay-Only", value) }
			replay, err := generationReplayOnly(headers)
			if (err != nil) != tc.invalid || replay != tc.replay { t.Fatal(replay, err) }
			if tc.invalid && domain.AsError(err).Code != "VALIDATION_FAILED" { t.Fatal(err) }
		})
	}
}
