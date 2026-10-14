package semanticproposal

import (
	"context"
	"errors"
	"fmt"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller holds the original parent lock. A private verified protection selects
// source authority; existing parent graphs use original transport-only recovery.
// Publication and registration remain outside the final admission transaction.
func (p TransferDraftPreparer) admitInitialPersonDefectParent(ctx context.Context, who auth.Identity, advanced nativeInitialDefectAdvance) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if advanced.phase != nativePreparationSucceeded {
		return zero, v2input.ErrInvalid
	}
	registered := advanced.protection
	plan := registered.published.preparation.plan
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || p.Configuration.Color.sha == "" {
		return zero, v2input.ErrInvalid
	}
	prepared := plan.prepared.dependencies.preparation
	d := prepared.landmarksPlan.detector
	original := prepared.person
	if original == nil {
		return zero, v2input.ErrConflict
	}
	if err := p.Reader.validateNativePersonInputAuthority(ctx, who, d.request, *original); err != nil {
		return zero, err
	}
	current, err := p.Reader.readRegisteredNativeInputProtection(ctx, who, registered.published, p.Configuration.Color)
	if err != nil {
		return zero, fmt.Errorf("initial native parent registered protection: %w", err)
	}
	if !sameRegisteredNativeInputProtection(ctx, current, registered) {
		return zero, v2input.ErrConflict
	}
	config, err := p.Reader.prepareNativeTransferRuntimeConfiguration(ctx, who, d.request.TenantID, ownTransferPreparation(p.Configuration))
	if err != nil {
		return zero, fmt.Errorf("initial native parent transfer runtime configuration: %w", err)
	}
	selected, err := selectQualificationPolicies(original.policy, []string{"mask.person"})
	if err != nil {
		return zero, fmt.Errorf("initial native parent person policy: %w", err)
	}
	config.PersonProtection = &TransferPersonProtection{Report: original.report, Expected: selected["mask.person"]}
	if config.SourceProtection != (v2input.Ref{}) && config.SourceProtection != registered.verified.Root {
		return zero, v2input.ErrConflict
	}
	config.SourceProtection = registered.verified.Root
	p.Configuration = config
	_, transferSource, err := p.Reader.readQualifiedSourcePreparationProof(ctx, who, d.request.TenantID, d.request.Intent.Reference, config.Color, false)
	if err != nil {
		return zero, fmt.Errorf("initial native parent transfer source: %w", err)
	}
	allNative, err := p.Reader.hasNativeAllTargetSavedParent(ctx, who, d.request)
	if err != nil {
		return zero, fmt.Errorf("initial native parent saved all-native routing: %w", err)
	}
	if allNative {
		return p.loadAndResumeNativeAllTargetTransferLocked(ctx, who, d.request, d.requestKey, config.Color)
	}
	person, err := p.Reader.prepareNativeInputReferencePerson(ctx, who, d.request, transferSource, registered, config.Color, original.report)
	if err != nil {
		return zero, fmt.Errorf("initial native parent reference person: %w", err)
	}
	built, err := recoverNativeInputDefectTransport(ctx, p.Scheduler, who, plan)
	if err != nil {
		return zero, fmt.Errorf("initial native parent defect transport: %w", err)
	}
	existing, err := p.Reader.hasNativeInputSavedParent(ctx, who, d.request)
	if err != nil {
		return zero, fmt.Errorf("initial native parent parent presence: %w", err)
	}
	if existing {
		p.Configuration, err = p.Reader.restoreNativeInputTransferProtectionConfiguration(ctx, who, d.request, p.Configuration)
		if err != nil {
			return zero, fmt.Errorf("initial native parent saved target protections: %w", err)
		}
		return p.loadAndResumeNativeInputTransfer(ctx, who, d.request.TenantID, d.requestKey, d.request.Body, person, built)
	}
	if len(p.Configuration.TargetProtection) == 0 {
		p.Configuration, err = p.Reader.selectTransferTargetProtectionConfiguration(ctx, who, d.request, d.published, p.Configuration)
		if err != nil {
			if errors.Is(err, errTransferProtectionUnavailable) {
				manifest, planErr := compileNativeTargetPreparationManifest(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody)
				if planErr != nil {
					return zero, fmt.Errorf("initial native target prerequisite manifest: %w", planErr)
				}
				return zero, &nativeTargetPreparationRequired{manifest: manifest}
			}
			return zero, fmt.Errorf("initial native parent target protection selection: %w", err)
		}
	} else if err := validateTransferConfiguration(p.Configuration); err != nil {
		return zero, fmt.Errorf("initial native parent pinned target configuration: %w", err)
	}
	// Server-pinned targets are locators only. prepareAndActivate still physically
	// verifies every target source, protection and person coverage before graph
	// publication, and repeats complete authority in admission SQL.
	ticket, err := p.prepareAndActivateNativeInputTransfer(ctx, who, d.request.TenantID, d.requestKey, d.request.Body, person, built)
	if err != nil {
		return zero, fmt.Errorf("initial native parent prepare/activate: %w", err)
	}
	return ticket, nil
}
