package v2draft

import (
	"crypto/sha256"
	"encoding/hex"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// BuildIdentityAdoptionInventory binds the only four artifacts reachable from
// IdentityReport: the root, its declared Calibration input, changed MaskRef,
// and the mask's NPY array. Paths match the V2 adoption walker exactly.
func BuildIdentityAdoptionInventory(evidence PreparedIdentityAdoption) ([]byte, string, error) {
	if !evidence.verified || evidence.Result.Report != evidence.Scope.Root ||
		evidence.Calibration.Ref != evidence.Inputs.Params.Calibration ||
		evidence.Result.Objects[0].SHA256 != evidence.Result.Report.SHA256 ||
		evidence.Result.Objects[1].SHA256 != evidence.Result.Mask.SHA256 ||
		evidence.Result.Objects[2].SHA256 != evidence.Result.Pixels.SHA256 {
		return nil, "", v2input.ErrInvalid
	}
	items := []struct {
		path string
		ref  v2input.Ref
	}{
		{"$", evidence.Result.Report},
		{"$/calibration", evidence.Calibration.Ref},
		{"$/changed_mask", evidence.Result.Mask},
		{"$/changed_mask/artifact", evidence.Result.Pixels},
	}
	entries := make([]DocumentRef, 0, len(items))
	seen := map[string]bool{}
	for ordinal, item := range items {
		if !isDocumentRef(item.ref) || seen[item.ref.ArtifactID.String()] {
			return nil, "", v2input.ErrInvalid
		}
		seen[item.ref.ArtifactID.String()] = true
		purpose := sha256.Sum256([]byte("tasks/" + evidence.Scope.Task.String() + "/" + item.path))
		entries = append(entries, DocumentRef{ArtifactID: item.ref.ArtifactID.String(),
			SHA256: item.ref.SHA256, Purpose: hex.EncodeToString(purpose[:]), Ordinal: ordinal})
	}
	raw, err := canonical.Marshal(entries)
	if err != nil || len(raw) > 16<<20 {
		return nil, "", v2input.ErrInvalid
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:]), nil
}
