package semanticreview

import "github.com/google/uuid"

// VerifiedRemovalPreviewEvidence binds numerical declarations to a canonical
// producer. Physical pixels and PNGs still require independent verification.
type VerifiedRemovalPreviewEvidence struct {
	TaskID, ResultID uuid.UUID
	Evidence         RemovalPreviewEvidence
	Source           PlanSource
	raw              []byte
}
