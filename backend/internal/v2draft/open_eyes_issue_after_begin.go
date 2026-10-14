package v2draft

import (
	"context"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type OpenEyesPreparationReader interface {
	PrepareProjectKeyed(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string,
		OpenEyesClientSelection) (OpenEyesClientPreparation, error)
}

type OpenEyesDraftBeginner interface {
	Begin(context.Context, OpenEyesDraftBeginRequest) (OpenEyesDraftIdentity, error)
}

type OpenEyesConfirmationIssuer interface {
	Issue(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID,
		v2portrait.OpenEyesParams, uuid.UUID, uuid.UUID) (v2portrait.OpenEyesParams, error)
}

type OpenEyesIssuedDraft struct {
	Identity OpenEyesDraftIdentity
	Params   v2portrait.OpenEyesParams
}

type OpenEyesDraftIssuer struct {
	Preparation OpenEyesPreparationReader
	Beginner    OpenEyesDraftBeginner
	Issuer      OpenEyesConfirmationIssuer
	Pin         v2portrait.DonorQualificationPin
	Reserved    int64
}

// IssueAfterBegin never accepts client-owned identity or consent fields. A
// changed preparation fails before a draft is made. After Begin commits, the
// existing donor issuer repeats current SQL authority at reserve and complete.
func (s OpenEyesDraftIssuer) IssueAfterBegin(ctx context.Context, who auth.Identity,
	tenant, project uuid.UUID, key string, intent OpenEyesClientIntent) (OpenEyesIssuedDraft, error) {
	var zero OpenEyesIssuedDraft
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Preparation == nil || s.Beginner == nil || s.Issuer == nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		tenant == uuid.Nil || project == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	selection := OpenEyesIntentSelection(intent)
	current, err := s.Preparation.PrepareProjectKeyed(ctx, who, tenant, project, key, selection)
	if err != nil {
		return zero, err
	}
	if current != intent {
		return zero, v2input.ErrExpired
	}
	request := OpenEyesDraftBeginRequest{Tenant: tenant, Project: project, Actor: who,
		Key: key, Intent: current, Pin: s.Pin, ReservedBytes: s.Reserved}
	identity, err := s.Beginner.Begin(ctx, request)
	if err != nil {
		return zero, err
	}
	issued, err := s.Issuer.Issue(ctx, who, tenant, project, current.Shoot,
		identity.Run, current.Proposal, current.SourceObservation, current.DonorObservation)
	if err != nil {
		return zero, err
	}
	if issued != current.Proposal || issued.Confirmation.ArtifactID != identity.Owner {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return OpenEyesIssuedDraft{Identity: identity, Params: issued}, nil
}
