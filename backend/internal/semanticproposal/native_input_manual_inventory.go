package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Selects only worker-declared root/target/protected files from independently
// verified manual proofs. Params/PNGs/vectors remain retained admission evidence,
// not extra undeclared worker downloads. This does not grant admission authority.
func nativeInputManualInventoryPins(ctx context.Context, selection nativeInputManualSelection, bucket string) ([]nativeDefectInputPin, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(selection.manuals) != len(selection.proofs) || len(selection.proofs) > 64 {
		return nil, v2input.ErrInvalid
	}
	known := map[uuid.UUID]nativeDefectInputPin{}
	for i, owned := range selection.proofs {
		proof := owned.original()
		if _, err := marshalManualEvidencePins(proof, bucket); err != nil {
			return nil, err
		}
		original := selection.manuals[i]
		sha := sha256.Sum256(original.Body)
		if original.Root.ID != proof.objects.Root.ref.ArtifactID || original.Root.SHA256 != proof.objects.Root.ref.SHA256 || hex.EncodeToString(sha[:]) != original.Root.SHA256 || int64(len(original.Body)) != proof.objects.Root.object.Bytes || original.Target.Artifact.ID != proof.objects.Pixel.ref.ArtifactID || original.Target.Artifact.SHA256 != proof.objects.Pixel.ref.SHA256 {
			return nil, v2input.ErrConflict
		}
		for _, pin := range []nativeDefectInputPin{{contract: "MaskRef", pin: proof.objects.Root}, {contract: "NpyArray", pin: proof.objects.Pixel}, {contract: "NpyArray", pin: proof.children[0]}} {
			if previous, exists := known[pin.pin.ref.ArtifactID]; exists && previous != pin {
				return nil, v2input.ErrConflict
			}
			known[pin.pin.ref.ArtifactID] = pin
		}
	}
	pins := make([]nativeDefectInputPin, 0, len(known))
	for _, pin := range known {
		pins = append(pins, pin)
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].pin.ref.ArtifactID.String() < pins[j].pin.ref.ArtifactID.String() })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return pins, nil
}
