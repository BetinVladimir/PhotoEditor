package workflow

import (
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

// The SQL resolver returns only decided review routes under the live Apply
// lease, including an explicitly chosen base. Artifact identity binds canvas,
// transform chain, pixels and color to that immutable adopted document.
func validateApplyCompositionQualityTarget(item uuid.UUID, target review.ArtifactRef, adopted []applyOutput, count int) error {
	if len(adopted) != count || count < 1 || item == uuid.Nil || target.ID == uuid.Nil {
		return errors.New("quality composition adoption missing")
	}
	seen := make(map[uuid.UUID]bool, count)
	found := false
	for _, output := range adopted {
		if output.ItemID == uuid.Nil || seen[output.ItemID] || output.Route != "candidate" && output.Route != "base" && output.Route != "skip" {
			return errors.New("quality composition adoption invalid")
		}
		seen[output.ItemID] = true
		if output.ItemID == item {
			found = output.Artifact.ref() == target
		}
	}
	if !found {
		return errors.New("quality composition target differs from adopted geometry")
	}
	return nil
}
