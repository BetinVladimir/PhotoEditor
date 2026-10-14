package interchange

import (
	"bytes"
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Implementations must qualify the exact source/runtime/native decoding and
// peak resources. Protocol floors or a caller-provided pass flag are insufficient.
type ExportResourceQualifier interface {
	QualifyExportResources(context.Context, CreateIntent, FrozenExportDocuments, ExportInputInventory) (ExportTaskResources, error)
}

// Private receipt: owned, freshly resolved inputs and a resource-qualified task.
// No database authority, dispatch or public creation ticket is implied.
type PreparedExportActivation struct {
	intent    CreateIntent
	frozen    FrozenExportDocuments
	inventory ExportInputInventory
	task      v2input.DraftTask
}
type ExportActivationPreparation struct {
	Resolver      ExportClosureNodeResolver
	Resources     ExportResourceQualifier
	PersonOrigins *ExportPersonOriginInputSet
}

func (p ExportActivationPreparation) Prepare(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, expected v2input.DraftTask) (PreparedExportActivation, error) {
	var zero PreparedExportActivation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Resolver == nil || p.Resources == nil {
		return zero, v2input.ErrInvalid
	}
	checked, compiled, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body())
	if err != nil {
		return zero, err
	}
	inventory, err := ResolveExportInputInventory(ctx, compiled, p.Resolver)
	if err != nil {
		return zero, err
	}
	if p.PersonOrigins != nil {
		inventory, err = p.PersonOrigins.Enrich(ctx, intent, checked, inventory)
		if err != nil {
			return zero, err
		}
	}
	resources, err := p.Resources.QualifyExportResources(ctx, copyExportIntent(intent), FrozenExportDocuments{body: checked.Body(), identity: checked.identity}, ExportInputInventory{task: inventory.task, entries: inventory.Entries(), digest: inventory.digest})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	task, err := CompileExportTask(ctx, intent, checked, inventory, resources)
	if err != nil {
		return zero, err
	}
	want, err := canonical.Marshal(expected)
	if err != nil {
		return zero, err
	}
	actual, err := canonical.Marshal(task)
	if err != nil || !bytes.Equal(want, actual) {
		return zero, v2input.ErrInvalid
	}
	return PreparedExportActivation{intent: copyExportIntent(intent), frozen: checked, inventory: inventory, task: task}, nil
}
