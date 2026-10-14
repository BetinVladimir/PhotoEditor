package interchange

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Must verify independently measured resources/current qualification against the
// exact newly resolved inventory. A requested profile is not measurement proof.
type ImportActivationResourceAuthority interface {
	QualifyImportActivationResources(context.Context, ImportPreparedDocuments, v2input.DraftTask, ImportInputInventory) error
}
type ImportActivationPreparation struct {
	Resolver  ImportClosureNodeResolver
	Resources ImportActivationResourceAuthority
	Reviews   *ImportReviewWitnesses
}

// Private evidence cannot be instantiated by HTTP JSON or a caller-supplied flag.
// Final SQL activation must still check current scope and stored transport pins.
type QualifiedImportActivation struct {
	tenant    uuid.UUID
	documents ImportPreparedDocuments
	task      v2input.DraftTask
	digest    string
	reviews   []ImportReviewWitness
}

func (p ImportActivationPreparation) Prepare(ctx context.Context, tenant uuid.UUID, documents ImportPreparedDocuments, task v2input.DraftTask) (QualifiedImportActivation, error) {
	var zero QualifiedImportActivation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tenant == uuid.Nil || p.Resolver == nil || p.Resources == nil || documents.evidence == nil {
		return zero, v2input.ErrInvalid
	}
	if err := validateImportTask(documents, task); err != nil {
		return zero, err
	}
	ownDocuments := func() ImportPreparedDocuments {
		d := documents
		d.plan.Body = append([]byte(nil), documents.plan.Body...)
		d.params.Body = append([]byte(nil), documents.params.Body...)
		evidence := *documents.evidence
		evidence.Body = append([]byte(nil), evidence.Body...)
		d.evidence = &evidence
		return d
	}
	documents = ownDocuments()
	task.ExecutionProfile = append(json.RawMessage(nil), task.ExecutionProfile...)
	before, err := ResolveImportInputInventory(ctx, tenant, ownDocuments(), p.Resolver)
	if err != nil {
		return zero, err
	}
	if before.InputDigest() != task.InputDigest {
		return zero, v2input.ErrInvalid
	}
	qualify := func(inventory ImportInputInventory) error {
		copied := task
		copied.ExecutionProfile = append(json.RawMessage(nil), task.ExecutionProfile...)
		inventory.entries = append([]v2input.Entry(nil), inventory.entries...)
		inventory.documents = ownDocuments()
		return p.Resources.QualifyImportActivationResources(ctx, ownDocuments(), copied, inventory)
	}
	if err := qualify(before); err != nil {
		return zero, err
	}
	after, err := ResolveImportInputInventory(ctx, tenant, ownDocuments(), p.Resolver)
	if err != nil {
		return zero, err
	}
	if before.InputDigest() != after.InputDigest() {
		return zero, v2input.ErrInvalid
	}
	if err := qualify(after); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	var reviewWitnesses []ImportReviewWitness
	if p.Reviews != nil {
		reviewWitnesses, err = p.Reviews.Snapshot(ctx)
		if err != nil {
			return zero, err
		}
	}
	return QualifiedImportActivation{tenant: tenant, documents: ownDocuments(), task: task, digest: after.InputDigest(), reviews: reviewWitnesses}, nil
}
