package v2draft

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type GlareDonorRestorationCoordinator struct {
	CatalogProject        uuid.UUID
	BaseAssetVersion      uuid.UUID
	DonorAssetVersion     uuid.UUID
	DB                    v2portrait.DonorConfirmationTransactions
	ConfirmationPublisher v2portrait.DonorConfirmationPublisher
	ParamsPublisher       v2input.DocumentPublisher
	TransportPublisher    v2input.DocumentPublisher
	BaseCatalog           ArtifactCatalog
	DonorCatalog          ArtifactCatalog
	Objects               v2input.SessionAnalysisObjectReader
}

type GlareDonorRestorationSubmission struct {
	TaskID    uuid.UUID
	ParamsRef v2input.Ref
	Params    []byte
	Ticket    RestorationAdmissionTicket
	Recovered bool
}

// Admit creates the complete one-task donor-glare draft: current observation
// choice -> server proof -> exact Params publication -> original manual mask
// reread -> derived physical inventory -> blocked task/transport -> graph seal.
// Every SQL transaction ends before object IO; each later phase repeats live
// authority rather than treating earlier evidence as a transferable grant.
func (c GlareDonorRestorationCoordinator) Admit(ctx context.Context,
	who auth.Identity, tenant, project, shoot, run, workflow, snapshot uuid.UUID,
	ordinal int, paramsProposal, baseProfile []byte,
	pin v2portrait.RestorationQualificationPin,
	sourceObservation, donorObservation uuid.UUID) (GlareDonorRestorationSubmission, error) {
	var zero GlareDonorRestorationSubmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.ConfirmationPublisher == nil || c.ParamsPublisher == nil ||
		c.TransportPublisher == nil || c.BaseCatalog == nil || c.DonorCatalog == nil ||
		c.Objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		tenant == uuid.Nil || project == uuid.Nil || shoot == uuid.Nil ||
		run == uuid.Nil || workflow == uuid.Nil || snapshot == uuid.Nil ||
		sourceObservation == uuid.Nil || donorObservation == uuid.Nil ||
		sourceObservation == donorObservation || pin.Operation != "portrait.glare" {
		return zero, v2input.ErrInvalid
	}
	if _, err := pin.Compile(); err != nil {
		return zero, err
	}
	if c.CatalogProject != uuid.Nil || c.BaseAssetVersion != uuid.Nil ||
		c.DonorAssetVersion != uuid.Nil {
		var versions struct {
			Base struct {
				AssetVersionID uuid.UUID `json:"asset_version_id"`
			} `json:"base"`
			Donor struct {
				AssetVersionID uuid.UUID `json:"asset_version_id"`
			} `json:"donor"`
		}
		if c.CatalogProject != project || c.BaseAssetVersion == uuid.Nil ||
			c.DonorAssetVersion == uuid.Nil ||
			json.Unmarshal(paramsProposal, &versions) != nil ||
			versions.Base.AssetVersionID != c.BaseAssetVersion ||
			versions.Donor.AssetVersionID != c.DonorAssetVersion {
			return zero, v2input.ErrInvalid
		}
	}
	profile, err := v2portrait.CompilePinnedRestorationProfile("portrait.glare",
		paramsProposal, baseProfile, pin)
	if err != nil {
		return zero, err
	}
	if _, err := v2input.TaskID(workflow, "portrait.glare", ordinal); err != nil {
		return zero, err
	}
	store := SQLRestorationDescriptorStore{DB: c.DB}
	if ticket, err := store.ReadAdmissionTicketForWorkflow(ctx, who, tenant,
		project, shoot, run, workflow, snapshot, ordinal, paramsProposal, profile,
		sourceObservation, donorObservation); err == nil {
		return GlareDonorRestorationSubmission{TaskID: ticket.TaskID,
			Ticket: ticket, Recovered: true}, nil
	} else if !errors.Is(err, v2input.ErrExpired) {
		return zero, err
	}
	params, err := v2portrait.IssueGlareDonorFromObservations(ctx, c.DB,
		c.ConfirmationPublisher, who, tenant, project, shoot, run, paramsProposal,
		sourceObservation, donorObservation)
	if err != nil {
		return zero, err
	}
	material, err := ResolveRestorationManualEvidence(ctx, c.DB, who,
		tenant, project, snapshot, "portrait.glare", params,
		c.BaseCatalog, c.DonorCatalog, c.Objects)
	if err != nil {
		return zero, err
	}
	paramsRef, err := (SQLRestorationParamsPublisher{DB: c.DB,
		Publisher: c.ParamsPublisher}).PublishDonorGlare(ctx, who, tenant, run,
		workflow, ordinal, params, pin, material.Origin)
	if err != nil {
		return zero, err
	}
	inventory, err := v2portrait.BuildRestorationInputInventory(ctx,
		"portrait.glare", params, paramsRef, pin, material.Documents)
	if err != nil {
		return zero, err
	}
	prepared, err := CompileResolvedRestorationDraftTask(ctx, c.DB, who,
		tenant, project, snapshot, workflow, ordinal, "portrait.glare",
		params, baseProfile, pin, paramsRef, inventory,
		c.BaseCatalog, c.DonorCatalog, c.Objects)
	if err != nil {
		return zero, err
	}
	if _, err := store.PublishAndRegisterTransport(ctx, who, tenant, run,
		prepared, params, c.TransportPublisher); err != nil {
		return zero, err
	}
	if err := store.AdmitRegistered(ctx, who, tenant, run, prepared, params); err != nil {
		if ticket, readErr := store.ReadAdmissionTicketForWorkflow(ctx, who,
			tenant, project, shoot, run, workflow, snapshot, ordinal, paramsProposal, profile,
			sourceObservation, donorObservation); readErr == nil &&
			ticket.TaskID == prepared.Draft.Task.ID {
			return GlareDonorRestorationSubmission{TaskID: ticket.TaskID,
				ParamsRef: paramsRef, Params: append([]byte(nil), params...),
				Ticket: ticket, Recovered: true}, nil
		}
		return zero, err
	}
	ticket, err := store.ReadAdmissionTicketForWorkflow(ctx, who, tenant,
		project, shoot, run, workflow, snapshot, ordinal, paramsProposal, profile,
		sourceObservation, donorObservation)
	if err != nil {
		return zero, err
	}
	if ticket.TaskID != prepared.Draft.Task.ID {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return GlareDonorRestorationSubmission{TaskID: prepared.Draft.Task.ID,
		ParamsRef: paramsRef, Params: append([]byte(nil), params...),
		Ticket: ticket}, nil
}
