package semanticproposal

import (
	"context"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// PinnedDocument must come from the actor's authorized tenant/project catalog.
// Object.VersionID is the catalog pin, never a latest-object lookup.
type PinnedDocument struct {
	Ref    v2input.Ref
	Object objectstore.Object
}

type TransferDocumentPins struct {
	Reference, Target, SelectedMask, SourceProtection, TargetProtection PinnedDocument
}

// ReadTransferDocuments owns all five exact document bodies. No partial bundle
// escapes after failure. Array payload and ICC qualification follow separately.
func ReadTransferDocuments(ctx context.Context, objects v2input.SessionAnalysisObjectReader, pins TransferDocumentPins) (v2input.SemanticPinnedDocuments, error) {
	documents := v2input.SemanticPinnedDocuments{}
	sources := []PinnedDocument{pins.Reference, pins.Target, pins.SelectedMask, pins.SourceProtection, pins.TargetProtection}
	bodies := make([][]byte, len(sources))
	for i, pin := range sources {
		body, err := v2input.ReadPinnedSemanticDocument(ctx, objects, pin.Object, pin.Ref)
		if err != nil {
			return v2input.SemanticPinnedDocuments{}, err
		}
		bodies[i] = body
	}
	documents.Reference, documents.Target, documents.SelectedMask, documents.SourceProtection, documents.TargetProtection = bodies[0], bodies[1], bodies[2], bodies[3], bodies[4]
	return documents, nil
}
