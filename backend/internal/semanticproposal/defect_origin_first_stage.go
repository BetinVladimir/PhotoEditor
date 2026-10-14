package semanticproposal

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type originFirstDefectPreparation struct {
	// Server-selected exact original policy digest; exclusive with retained pins.
	paramsSHA                            string
	producerID                           uuid.UUID
	origin                               DefectOriginPins
	person                               personProtectionProof
	costs                                v2input.SemanticQualifiedCosts
	ram, vram, scratch, output, reserved int64
}

// Reconstructs descriptors and qualification from the original canonical
// producer, then enters the existing first-stage admission/recovery path. This
// returns an internal child ticket; it does not implement public parent Propose.
func (p TransferDraftPreparer) prepareAndAdmitOriginDefectStage(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, native originFirstDefectPreparation) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	// Apply the worker's fixed lower resource bound before either live producer
	// discovery or saved-locator SQL. Full source/input accounting follows later.
	if _, err := v2input.CompileSemanticDefectProfile(8, 8, 1, native.costs, native.ram, native.vram, native.scratch, native.output); err != nil {
		return zero, err
	}
	if native.paramsSHA == "" && native.producerID != uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if native.paramsSHA != "" {
		if native.origin != (DefectOriginPins{}) || native.producerID == uuid.Nil {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(native.paramsSHA); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	if p.Scheduler == nil || p.TransportDocuments == nil || who.SessionID == uuid.Nil || request.ActorID != who.UserID || len(snapshotBody) < 2 || len(snapshotBody) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	// Freeze command and mutable parent collections before any evidence IO.
	owned, err := CompileRequest(request.TenantID, who.UserID, key, request.Body)
	if err != nil {
		return zero, err
	}
	if owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA {
		return zero, v2input.ErrInvalid
	}
	snapshotBody = append([]byte(nil), snapshotBody...)
	published.Inventory.Targets = append([]v2input.SessionVersionBinding(nil), published.Inventory.Targets...)
	intentSHA, err := canonical.Hash(owned.Intent)
	if err != nil || published.Ref.ArtifactID == uuid.Nil || published.Ref.SHA256 != intentSHA {
		return zero, v2input.ErrInvalid
	}
	targets, err := BindTargets(owned, snapshot, snapshotBody)
	if err != nil {
		return zero, err
	}
	if published.Inventory.Scope.ProjectID != p.Reader.ProjectID || len(targets) != len(published.Inventory.Targets) || len(targets) != published.Inventory.Scope.ItemCount {
		return zero, v2input.ErrInvalid
	}
	for i, target := range targets {
		if target != published.Inventory.Targets[i] {
			return zero, v2input.ErrConflict
		}
	}
	if native.paramsSHA != "" {
		native.origin, err = p.Reader.discoverDefectOriginForProducer(ctx, who, owned.TenantID, owned.Intent.Reference, native.producerID, native.paramsSHA)
		if err != nil {
			return zero, err
		}
	} else if native.origin == (DefectOriginPins{}) {
		// Restart-only path: no SHA-only search and no fresh-source fallback.
		// The stored locator is followed by all current qualification/content
		// checks and by the final locked config-to-producer identity gate.
		native.origin, err = p.Reader.readSavedDefectOrigin(ctx, who, owned, published.Ref, snapshot)
		if err != nil {
			return zero, err
		}
	}
	execution, err := p.Reader.prepareOriginSelectedDefectExecution(ctx, who, owned.TenantID, owned.Intent.Reference, p.Configuration.Color, native.origin, native.costs, native.ram, native.vram, native.scratch, native.output)
	if err != nil {
		return zero, fmt.Errorf("original defect execution preparation: %w", err)
	}
	ticket, err := p.prepareAndAdmitFirstDefectStage(ctx, who, owned, key, published, snapshot, snapshotBody, firstDefectPreparation{execution: execution, person: native.person, costs: native.costs, ram: native.ram, vram: native.vram, scratch: native.scratch, output: native.output, reserved: native.reserved})
	if err != nil {
		return zero, fmt.Errorf("original defect stage admission/recovery: %w", err)
	}
	return ticket, nil
}
