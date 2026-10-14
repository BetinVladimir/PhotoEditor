package v2portrait

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// DonorConfirmationPublication is the immutable request to a dedicated issuer
// repository. It is not compatible with generic task_params publication.
type DonorConfirmationPublication struct {
	Actor                                auth.Identity
	Tenant, Project, ShootSession, RunID uuid.UUID
	SourceObservation, DonorObservation  uuid.UUID
	Prepared                             PreparedDonorConfirmation
}

// DonorConfirmationPublisher must reserve a replay-stable ID under server issuer
// provenance, recheck current authority at reserve/complete, account storage,
// fence completion and return a ready pinned document. Implementations must not
// use a tenant-only artifact lookup. The production repository is still pending.
type DonorConfirmationPublisher interface {
	PublishDonorConfirmation(context.Context, DonorConfirmationPublication) (v2input.Ref, error)
}

// IssueOpenEyesConfirmation releases DB locks before object IO and rechecks the
// same proof after publication. A revoked/changed grant returns no accepted ref;
// already uploaded objects require repository orphan cleanup independently.
func IssueOpenEyesConfirmation(ctx context.Context, db DonorConfirmationTransactions,
	publisher DonorConfirmationPublisher, who auth.Identity, tenant, project, shoot, run uuid.UUID,
	params OpenEyesParams, sourceObservation, donorObservation uuid.UUID) (OpenEyesParams, error) {
	var zero OpenEyesParams
	if publisher == nil || run == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	prepared, err := PrepareOpenEyesConfirmation(ctx, db, who, tenant, project, shoot, params, sourceObservation, donorObservation)
	if err != nil {
		return zero, err
	}
	owned := prepared
	owned.Body = append([]byte(nil), prepared.Body...)
	ref, err := publisher.PublishDonorConfirmation(ctx, DonorConfirmationPublication{Actor: who, Tenant: tenant, Project: project, ShootSession: shoot, RunID: run,
		SourceObservation: sourceObservation, DonorObservation: donorObservation, Prepared: owned})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if ref.ArtifactID == uuid.Nil || ref.SHA256 != prepared.SHA256 {
		return zero, v2input.ErrInvalid
	}
	for _, input := range []v2input.Ref{params.Base.Source, params.Donor.Source, params.EyesMask, params.Protected, params.SourceFaces, params.DonorFaces} {
		if input.ArtifactID == ref.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	bound := prepared.Params
	bound.Confirmation = ref
	current, err := PrepareOpenEyesConfirmation(ctx, db, who, tenant, project, shoot, bound, sourceObservation, donorObservation)
	if err != nil {
		return zero, err
	}
	if current.SHA256 != prepared.SHA256 || !bytes.Equal(current.Body, prepared.Body) {
		return zero, v2input.ErrExpired
	}
	return bound, nil
}
