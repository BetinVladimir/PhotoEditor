package v2portrait

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type DonorConfirmationReservation struct {
	RunID, ArtifactID, Token uuid.UUID
	Ready                    *v2input.Ref
}

// Repository authorizes issuer scope, accounts quota, allocates stable IDs and
// fences completion. It must prove request.RunID belongs to the active tenant,
// authenticated actor and project/workflow, and preserve this exact run in its
// reservation. Each call commits independently of object IO.
type DonorConfirmationRepository interface {
	ReserveDonorConfirmation(context.Context, DonorConfirmationPublication, int64) (DonorConfirmationReservation, error)
	CompleteDonorConfirmation(context.Context, DonorConfirmationPublication, DonorConfirmationReservation, objectstore.Object) (v2input.Ref, error)
}

type DonorConfirmationObjectPublisher struct {
	Repository DonorConfirmationRepository
	Objects    artifactjson.Objects
}

func (p DonorConfirmationObjectPublisher) PublishDonorConfirmation(ctx context.Context, request DonorConfirmationPublication) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Repository == nil || p.Objects == nil || request.Tenant == uuid.Nil || request.Project == uuid.Nil || request.ShootSession == uuid.Nil || request.RunID == uuid.Nil || request.Actor.UserID == uuid.Nil || request.Actor.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	request.Prepared.Body = append([]byte(nil), request.Prepared.Body...)
	proof, err := VerifyOpenEyesConfirmation(request.Prepared.Params, request.Prepared.Body)
	if err != nil {
		return zero, err
	}
	if request.Prepared.SHA256 != request.Prepared.Params.Confirmation.SHA256 || proof.ActorID != request.Actor.UserID || proof.SourceObservationID != request.SourceObservation || proof.DonorObservationID == nil || *proof.DonorObservationID != request.DonorObservation {
		return zero, v2input.ErrInvalid
	}
	// Own bytes separately so repository callbacks cannot change uploaded content.
	body := append([]byte(nil), request.Prepared.Body...)
	size := int64(len(body))
	digest := request.Prepared.SHA256
	reservation, err := p.Repository.ReserveDonorConfirmation(ctx, request, size)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if reservation.RunID != request.RunID {
		return zero, v2input.ErrInvalid
	}
	key, err := objectstore.DocumentKey(request.Tenant, reservation.RunID, reservation.ArtifactID)
	if err != nil {
		return zero, err
	}
	expected := v2input.Ref{ArtifactID: reservation.ArtifactID, SHA256: digest}
	for _, ref := range []v2input.Ref{request.Prepared.Params.Base.Source, request.Prepared.Params.Donor.Source, request.Prepared.Params.EyesMask, request.Prepared.Params.Protected, request.Prepared.Params.SourceFaces, request.Prepared.Params.DonorFaces} {
		if ref.ArtifactID == expected.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	if reservation.Ready != nil {
		if *reservation.Ready != expected {
			return zero, objectstore.ErrIntegrity
		}
		return expected, nil
	}
	if reservation.Token == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	object, err := p.Objects.PutImmutable(ctx, key, bytes.NewReader(body), size, digest, "application/json")
	if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		object, err = p.Objects.RecoverImmutable(ctx, key, size, digest, "application/json")
	}
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if object.Key != key || object.SHA256 != digest || object.Bytes != size || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" {
		return zero, objectstore.ErrIntegrity
	}
	request.Prepared.Body = append([]byte(nil), body...)
	result, err := p.Repository.CompleteDonorConfirmation(ctx, request, reservation, object)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if result != expected {
		return zero, objectstore.ErrIntegrity
	}
	return result, nil
}
