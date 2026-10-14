package delivery

import (
	"github.com/google/uuid"
)

// NewChildExportID is the retry-stable identity for a newly admitted export.
// It does not authorize admission or select an existing cached export. Cache
// reuse requires its own ownership/readiness checks and persisted binding.
func (p PreparePlan) NewChildExportID(parent uuid.UUID, child ExportChildIntent) (uuid.UUID, error) {
	if _, err := p.ChildUsesWatermark(parent, child); err != nil {
		return uuid.Nil, err
	}
	return uuid.NewSHA1(child.RunID, []byte("v30:delivery-export:1")), nil
}
