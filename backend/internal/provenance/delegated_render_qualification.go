package provenance

import (
 "context"

 "photoeditor/backend/internal/v2draft"
)

type DelegatedRenderQualificationReader interface {
 ReadDelegatedRenderQualification(context.Context,CanonicalSigningTask,
  v2draft.FrozenRenderRecipe)(SigningRenderQualification,error)
}
