package semanticreview

import "github.com/google/uuid"

// VerifiedBackgroundCleanupPreviewEvidence binds numerical declarations to a canonical
// producer. Physical pixels and PNGs still require independent verification.
type VerifiedBackgroundCleanupPreviewEvidence struct {
	TaskID, ResultID uuid.UUID
	Evidence         BackgroundCleanupPreviewEvidence
	Source           PlanSource
	raw              []byte
}
