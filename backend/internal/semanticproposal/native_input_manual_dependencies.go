package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type nativeInputManualDependencies struct {
	producers             nativeInputRestoredFaceProducers
	selection             nativeInputManualSelection
	landmarks, skin, seal string
}

// Object preflight runs before the caller opens the admission transaction.
// This packet composes original producer and manual evidence only; qualification,
// Params registration and protected-source policy still need their own authority.
func (s SQLTransferSourceReader) prepareNativeInputManualDependencies(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, selection nativeInputManualSelection, color TransferColorProfile, transports nativeInputFaceProducerTransports) (nativeInputManualDependencies, error) {
	var zero nativeInputManualDependencies
	proof, err := s.assembleNativeInputManualDependencies(ctx, who, producers, selection, color, transports)
	if err != nil {
		return zero, err
	}
	current, err := s.readNativeInputSavedManualInputs(ctx, who, producers, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(current, selection) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return proof, nil
}

// Shared original producer packet construction. The calling initial or recovery
// path must independently repeat its accepted Params/manual selection authority.
func (s SQLTransferSourceReader) assembleNativeInputManualDependencies(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, selection nativeInputManualSelection, color TransferColorProfile, transports nativeInputFaceProducerTransports) (nativeInputManualDependencies, error) {
	var zero nativeInputManualDependencies
	if err := verifyNativeInputManualProducerBindings(ctx, who, producers, selection); err != nil {
		return zero, err
	}
	landmarks, err := s.marshalNativeLandmarkCompletedEvidence(ctx, who, producers.landmarks, color, selection.landmarks, transports.landmarks, transports.detector)
	if err != nil {
		return zero, err
	}
	skin, err := s.marshalNativeSkinCompletedEvidence(ctx, who, producers.skin, color, selection.skin, transports.skin, transports.parser, transports.detector)
	if err != nil {
		return zero, err
	}
	proof := nativeInputManualDependencies{producers: producers, selection: selection, landmarks: string(landmarks), skin: string(skin)}
	if err := s.verifyNativeInputManualCompletedPackets(ctx, proof); err != nil {
		return zero, err
	}
	proof.seal, err = s.nativeInputManualDependencyDigest(ctx, proof)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return proof, nil
}

func (s SQLTransferSourceReader) nativeInputManualDependencyDigest(ctx context.Context, proof nativeInputManualDependencies) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(proof.selection.proofs) > 64 || proof.landmarks == "" || proof.skin == "" {
		return "", v2input.ErrInvalid
	}
	pins := make([]string, len(proof.selection.proofs))
	for i, original := range proof.selection.proofs {
		body, err := marshalManualEvidencePins(original.original(), s.Bucket)
		if err != nil {
			return "", err
		}
		pins[i] = string(body)
	}
	selection := proof.selection
	return canonical.Hash(map[string]any{"producer_seals": selection.producerSeals, "tenant": selection.tenant, "project": selection.project, "actor": selection.actor, "session": selection.session, "parent": selection.parent, "binding": selection.binding, "params_ref": selection.paramsRef, "params": selection.params.params, "inputs": selection.params.inputs, "roots": selection.params.roots, "source": selection.sourceBody, "manual_evidence": pins, "landmarks": proof.landmarks, "skin": proof.skin})
}

// Locks original completed FAN then skin then all original manual evidence in
// one transaction. Caller must roll back every error. No graph writes or IO.
func (s SQLTransferSourceReader) lockNativeInputManualDependenciesInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, proof nativeInputManualDependencies) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || proof.seal == "" {
		return v2input.ErrInvalid
	}
	if err := verifyNativeInputManualProducerBindings(ctx, who, proof.producers, proof.selection); err != nil {
		return err
	}
	digest, err := s.nativeInputManualDependencyDigest(ctx, proof)
	if err != nil {
		return err
	}
	if digest != proof.seal {
		return v2input.ErrConflict
	}
	if err := s.verifyNativeInputManualCompletedPackets(ctx, proof); err != nil {
		return err
	}
	if err := s.lockNativeCompletedLandmarksInTransaction(ctx, tx, who, proof.producers.landmarks, []byte(proof.landmarks)); err != nil {
		return err
	}
	if err := s.lockNativeCompletedSkinInTransaction(ctx, tx, who, proof.producers.skin, []byte(proof.skin)); err != nil {
		return err
	}
	return s.lockNativeInputManualSelectionInTransaction(ctx, tx, who, proof.producers, proof.selection)
}
