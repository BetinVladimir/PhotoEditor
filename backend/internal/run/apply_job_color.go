package run

import (
 "encoding/json"
 "photoeditor/backend/internal/stylevalidation"
)

// Preserve package-local admission entry points while sharing strict contracts.
func validApplyJobColor(raw json.RawMessage) bool { return stylevalidation.Color(raw) }
