package workflow

import (
	"encoding/json"
	"photoeditor/backend/internal/styleplan"
)

// Preserve the scheduler contract while keeping the pure plan compiler free
// of scheduler/service dependencies for API and run preparation callers.
type AuthoredBasePlan = styleplan.AuthoredBasePlan

func CompileAuthoredBasePlan(raw json.RawMessage, itemCount int) (AuthoredBasePlan, error) {
	return styleplan.CompileAuthoredBasePlan(raw, itemCount)
}
