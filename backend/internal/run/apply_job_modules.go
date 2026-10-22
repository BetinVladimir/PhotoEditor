package run

import (
 "encoding/json"
 "photoeditor/backend/internal/stylevalidation"
)

// Preserve package-local admission entry points while sharing strict contracts.
func validApplyJobModule(name string, body json.RawMessage) bool { return stylevalidation.Module(name,body) }
