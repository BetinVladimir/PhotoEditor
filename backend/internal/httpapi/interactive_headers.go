package httpapi

import (
	"net/http"
	"photoeditor/backend/internal/domain"
)

// A replay command has one unambiguous mode. Repeated or empty headers must
// not silently select the first value and turn a malformed command into work.
func generationReplayOnly(headers http.Header) (bool, error) {
	values := headers.Values("Generation-Replay-Only")
	if len(values) == 0 { return false, nil }
	if len(values) != 1 || values[0] != "true" {
		return false, domain.NewError("VALIDATION_FAILED", 422)
	}
	return true, nil
}
