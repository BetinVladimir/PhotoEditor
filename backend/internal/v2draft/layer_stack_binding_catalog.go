package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// LayerStackBindingCatalog supplies a binding only after Lookup has authorized
// this exact occurrence. Implementations must derive it from authenticated
// published parent bytes. Ordinary catalogs retain original-source validation.
type LayerStackBindingCatalog interface {
	LayerStackBinding(context.Context, auth.Identity, uuid.UUID, LayerRef) (v2input.SessionVersionBinding, bool, error)
}
