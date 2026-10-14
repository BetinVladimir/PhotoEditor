package provenance

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// Explicit human acceptance of this exact adopted negative result. This is
// distinct from the algorithm's required review status and transfer approval.
type SigningNegativeReview struct {
 ID,Task,Result uuid.UUID
 Root,Report v2input.Ref
 Accepted bool
}
type SigningNegativeReviewReader interface {
 ReadSigningNegativeReview(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask,v2input.Ref,v2input.Ref)(*SigningNegativeReview,error)
}
