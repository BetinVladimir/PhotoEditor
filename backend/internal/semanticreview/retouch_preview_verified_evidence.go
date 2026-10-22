package semanticreview

import "github.com/google/uuid"

// VerifiedRetouchPreviewEvidence binds numerical declarations to a canonical
// producer. Physical pixels and PNGs still require independent verification.
type VerifiedRetouchPreviewEvidence struct {
	TaskID, ResultID uuid.UUID
	Evidence         RetouchPreviewEvidence
	Source           PlanSource
	raw              []byte
}
