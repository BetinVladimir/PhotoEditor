package run

import (
 "encoding/json"
 "photoeditor/backend/internal/stylevalidation"
)

// Preserve package-local admission entry points while sharing strict contracts.
func validApplyJobExtensions(raw json.RawMessage) bool { return stylevalidation.Extensions(raw) }
