package v2portrait

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type donorIssuerContext struct {
	SchemaVersion     int            `json:"schema_version"`
	Tenant            uuid.UUID      `json:"tenant_id"`
	Run               uuid.UUID      `json:"run_id"`
	Project           uuid.UUID      `json:"project_id"`
	Shoot             uuid.UUID      `json:"shoot_session_id"`
	Actor             uuid.UUID      `json:"actor_id"`
	AuthSession       uuid.UUID      `json:"auth_session_id"`
	SourceObservation uuid.UUID      `json:"source_observation_id"`
	DonorObservation  uuid.UUID      `json:"donor_observation_id"`
	Params            OpenEyesParams `json:"params"`
	ProofJCS          string         `json:"proof_jcs"`
}

// DonorIssuerContext produces the exact persisted lineage validated by SQL.
// This document is backend-only and cannot authorize publication on its own.
func DonorIssuerContext(request DonorConfirmationPublication) ([]byte, error) {
	if request.Tenant == uuid.Nil || request.RunID == uuid.Nil || request.Project == uuid.Nil || request.ShootSession == uuid.Nil || request.Actor.UserID == uuid.Nil || request.Actor.SessionID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	if _, err := DonorDirectInputs(request.Prepared.Params); err != nil {
		return nil, err
	}
	proof, err := VerifyOpenEyesConfirmation(request.Prepared.Params, request.Prepared.Body)
	if err != nil {
		return nil, err
	}
	if request.Prepared.SHA256 != request.Prepared.Params.Confirmation.SHA256 || proof.ActorID != request.Actor.UserID || proof.SourceObservationID != request.SourceObservation || proof.DonorObservationID == nil || *proof.DonorObservationID != request.DonorObservation {
		return nil, v2input.ErrInvalid
	}
	return canonical.Marshal(donorIssuerContext{1, request.Tenant, request.RunID, request.Project, request.ShootSession, request.Actor.UserID, request.Actor.SessionID, request.SourceObservation, request.DonorObservation, request.Prepared.Params, string(request.Prepared.Body)})
}
