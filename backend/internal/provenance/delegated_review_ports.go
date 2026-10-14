package provenance

import (
 "context"
 "photoeditor/backend/internal/v2input"
)

type DelegatedRenderReviewPort interface{
 ReadDelegatedRenderReview(context.Context,CanonicalSigningTask,
  v2input.Ref,v2input.Ref,bool)(*SigningRenderReview,error)
}
type DelegatedNegativeReviewPort interface{
 ReadDelegatedNegativeReview(context.Context,CanonicalSigningTask,
  v2input.Ref,v2input.Ref)(*SigningNegativeReview,error)
}
