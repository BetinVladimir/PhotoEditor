package semanticproposal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type SnapshotRecipe struct {
	Ref  v2input.Ref
	Body json.RawMessage
}

// CompileTargetSnapshot binds each exact canonical EditingRecipeV3 to its
// ordered catalog source. The caller proves live catalog ownership and recipe
// selection; content validation alone does not establish that authority.
func CompileTargetSnapshot(snapshotID uuid.UUID, inventory CatalogInventory, recipes []SnapshotRecipe) (v2input.SessionInputSnapshot, []byte, error) {
	if inventory.Scope.ProjectID == uuid.Nil || inventory.Scope.ItemCount != len(inventory.Targets) || len(recipes) != len(inventory.Targets) {
		return v2input.SessionInputSnapshot{}, nil, v2input.ErrInvalid
	}
	items := make([]v2input.SessionSnapshotItem, len(recipes))
	for i, recipe := range recipes {
		raw := append([]byte(nil), recipe.Body...)
		sum := sha256.Sum256(raw)
		normalized, err := canonical.Bytes(raw)
		if recipe.Ref.ArtifactID == uuid.Nil || len(raw) < 2 || len(raw) > 1<<20 || hex.EncodeToString(sum[:]) != recipe.Ref.SHA256 || err != nil || !bytes.Equal(normalized, raw) || v2input.ValidateEditingRecipeV3(raw, inventory.Targets[i]) != nil {
			return v2input.SessionInputSnapshot{}, nil, v2input.ErrInvalid
		}
		items[i] = v2input.SessionSnapshotItem{Ordinal: i, Binding: inventory.Targets[i], WorkingImage: inventory.Targets[i].Source, Recipe: recipe.Ref}
	}
	return v2input.CompilePinnedSessionInputSnapshot(snapshotID, items)
}
