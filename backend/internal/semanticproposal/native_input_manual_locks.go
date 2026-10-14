package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Locks complete original manual evidence against its retained native request
// scope. The caller must also lock native model/source/policy/Params evidence and
// roll back every error; this component neither registers nor activates a graph.
func (s SQLTransferSourceReader) lockNativeInputManualSelectionInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, producers nativeInputRestoredFaceProducers, selection nativeInputManualSelection) error {
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return err
	}
	d := producers.detector
	if tx == nil || selection.tenant != d.request.TenantID || selection.project != s.ProjectID || selection.actor != who.UserID || selection.session != who.SessionID || selection.parent != d.request.RunID || selection.binding != d.request.Intent.Reference {
		return v2input.ErrInvalid
	}
	if err := verifyNativeInputManualProducerBindings(ctx, who, producers, selection); err != nil {
		return err
	}
	pins, err := nativeInputManualInventoryPins(ctx, selection, s.Bucket)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(pins, selection.pins) {
		return v2input.ErrConflict
	}
	wanted := map[v2input.Ref]bool{}
	for _, face := range selection.policy.Faces {
		if face.Manual != nil {
			wanted[v2input.Ref{ArtifactID: face.Manual.Artifact.ID, SHA256: face.Manual.Artifact.SHA256}] = true
		}
	}
	if len(wanted) != len(selection.proofs) {
		return v2input.ErrConflict
	}
	proofs := make([]manualPreparationProof, len(selection.proofs))
	for i, owned := range selection.proofs {
		proof := owned.original()
		if !wanted[proof.objects.Pixel.ref] || !validManualPreparationScope(s.ProjectID, who, d.request.TenantID, d.request.Intent.Reference, proof) {
			return v2input.ErrConflict
		}
		delete(wanted, proof.objects.Pixel.ref)
		proofs[i] = proof
	}
	if len(wanted) != 0 {
		return v2input.ErrConflict
	}
	if len(proofs) == 0 {
		return ctx.Err()
	}
	return s.lockManualPreparationProofs(ctx, tx, who, d.request.TenantID, d.request.Intent.Reference, proofs)
}
