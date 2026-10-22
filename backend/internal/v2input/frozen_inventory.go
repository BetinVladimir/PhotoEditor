package v2input

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
)

// FreezeInputInventory constructs a version-1 transport inventory only. It does
// not register, authorize or admit an operation. Domain compilers must derive
// entries from verified documents and enforce their operation-specific schema.
// The caller's slice is copied before sorting and retained state is private.
func FreezeInputInventory(task uuid.UUID, operation string, params json.RawMessage,
	paramsRef Ref, entries []Entry) (CaptureClosure, error) {
	if task == uuid.Nil || len(operation) < 1 || len(operation) > 120 || strings.TrimSpace(operation) != operation ||
		len(params) < 2 || len(params) > 1048576 || len(entries) < 1 || len(entries) > 512 {
		return CaptureClosure{}, ErrInvalid
	}
	for _, r := range operation {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_') {
			return CaptureClosure{}, ErrInvalid
		}
	}
	found := false
	for _, entry := range entries {
		if entry.Ref == paramsRef && entry.Contract == "Params" {
			found = true
		}
	}
	if !found {
		return CaptureClosure{}, ErrInvalid
	}
	return compileCaptureInventory(task, operation, json.RawMessage(append([]byte(nil), params...)), paramsRef, append([]Entry(nil), entries...))
}
