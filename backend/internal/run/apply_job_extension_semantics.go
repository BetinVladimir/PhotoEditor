package run

import (
 "encoding/json"
 "photoeditor/backend/internal/stylevalidation"
)

// Preserve package-local admission entry points while sharing strict contracts.
func validApplyJobExtension(name string, raw json.RawMessage) bool { return stylevalidation.Extension(name,raw) }
