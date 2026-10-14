package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type nativeDefectDependencies struct {
	preparation           nativeDefectFacePreparation
	landmarks, skin, seal string
}

// Performs all object reads before the caller opens a parent transaction.
func (s SQLTransferSourceReader) prepareNativeDefectDependencies(ctx context.Context, who auth.Identity, prepared nativeDefectFacePreparation, color TransferColorProfile, landmarkBuilt, skinBuilt, parserBuilt, detectorBuilt v2input.Built) (nativeDefectDependencies, error) {
	var zero nativeDefectDependencies
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if prepared.landmarksPlan.detector.seal != prepared.skinPlan.parser.detector.seal || prepared.params.params == "" {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeDefectPreparedParams(ctx, prepared); err != nil {
		return zero, err
	}
	if err := s.recheckNativeDefectPersonInput(ctx, who, prepared); err != nil {
		return zero, err
	}
	landmarks, err := s.marshalNativeLandmarkCompletedEvidence(ctx, who, prepared.landmarksPlan, color, prepared.landmarks, landmarkBuilt, detectorBuilt)
	if err != nil {
		return zero, err
	}
	skin, err := s.marshalNativeSkinCompletedEvidence(ctx, who, prepared.skinPlan, color, prepared.skin, skinBuilt, parserBuilt, detectorBuilt)
	if err != nil {
		return zero, err
	}
	if err := s.recheckNativeDefectPersonInput(ctx, who, prepared); err != nil {
		return zero, err
	}
	proof := nativeDefectDependencies{preparation: prepared, landmarks: string(landmarks), skin: string(skin)}
	proof.seal, err = nativeDefectDependencyDigest(proof)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return proof, nil
}

func nativeDefectDependencyDigest(proof nativeDefectDependencies) (string, error) {
	fields := map[string]any{"landmarks_plan": proof.preparation.landmarksPlan.seal, "skin_plan": proof.preparation.skinPlan.seal, "params": proof.preparation.params.params, "params_sha": proof.preparation.params.digest, "inputs": proof.preparation.params.inputs, "manual_roots": proof.preparation.params.roots, "landmarks": proof.landmarks, "skin": proof.skin}
	if proof.preparation.person != nil {
		digest, err := nativePersonInputAuthorityDigest(*proof.preparation.person)
		if err != nil {
			return "", err
		}
		if proof.preparation.person.seal == "" || digest != proof.preparation.person.seal {
			return "", v2input.ErrConflict
		}
		fields["original_person_input"] = digest
	}
	return canonical.Hash(fields)
}

// Locks both completed producers in deterministic FAN-then-skin order in the
// same transaction. Errors must roll back the transaction; this helper does not
// authorize defects qualification, Params registration or task activation.
func (s SQLTransferSourceReader) lockNativeDefectDependenciesInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, proof nativeDefectDependencies) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || proof.seal == "" || proof.landmarks == "" || proof.skin == "" {
		return v2input.ErrInvalid
	}
	digest, err := nativeDefectDependencyDigest(proof)
	if err != nil {
		return err
	}
	if digest != proof.seal || proof.preparation.landmarksPlan.detector.seal != proof.preparation.skinPlan.parser.detector.seal {
		return v2input.ErrConflict
	}
	if err := verifyNativeDefectPreparedParams(ctx, proof.preparation); err != nil {
		return err
	}
	if err := s.lockNativeCompletedLandmarksInTransaction(ctx, tx, who, proof.preparation.landmarksPlan, []byte(proof.landmarks)); err != nil {
		return err
	}
	if err := s.lockNativeCompletedSkinInTransaction(ctx, tx, who, proof.preparation.skinPlan, []byte(proof.skin)); err != nil {
		return err
	}
	if proof.preparation.person != nil {
		return s.lockNativePersonInputAuthority(ctx, tx, who, proof.preparation.landmarksPlan.detector.request, *proof.preparation.person)
	}
	return ctx.Err()
}
