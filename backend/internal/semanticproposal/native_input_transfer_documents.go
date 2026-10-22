package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeInputTransferReferenceDocuments struct {
	reference, selected, protection json.RawMessage
	person                          nativeInputReferencePerson
	selection                       nativeInputSelectionSeparationProof
}

// Creates compiler documents only after independent person coverage and
// nonempty zero-overlap selection, retaining the original native-input authority.
func (s SQLTransferSourceReader) readNativeInputTransferReferenceDocuments(ctx context.Context, who auth.Identity, request Request, person nativeInputReferencePerson, color TransferColorProfile, budget int64) (nativeInputTransferReferenceDocuments, error) {
	var result nativeInputTransferReferenceDocuments
	err := nativeReadOperation(ctx, s, who, color, func(scoped SQLTransferSourceReader) error {
		var err error
		result, err = scoped.readNativeInputTransferReferenceDocumentsInScope(ctx, who, request, person, color, budget)
		return err
	})
	if err != nil {
		return nativeInputTransferReferenceDocuments{}, err
	}
	return result, nil
}

func (s SQLTransferSourceReader) readNativeInputTransferReferenceDocumentsInScope(ctx context.Context, who auth.Identity, request Request, person nativeInputReferencePerson, color TransferColorProfile, budget int64) (nativeInputTransferReferenceDocuments, error) {
	var zero nativeInputTransferReferenceDocuments
	if err := ctx.Err(); err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	if s.DB == nil || s.Objects == nil || budget <= 0 || budget > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	current, err := s.prepareNativeInputReferencePersonMode(ctx, who, request, person.source.source, person.source.protection, color, person.person.report, person.originalPinned)
	if err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	if !sameNativeInputReferencePerson(ctx, current, person) {
		return zero, fmt.Errorf("reference person changed before IO: %w", v2input.ErrConflict)
	}
	_, selection, err := s.verifyNativeInputSelectionSeparation(ctx, who, request, person.source, color)
	if err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	if _, err := s.marshalNativeInputSelectionPins(who, request, selection); err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	pins, err := decodeSourcePreparationPins(person.source.source)
	if err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	objects := []personEvidenceObject{
		{ref: request.Intent.Reference.Source, object: pins[0].Object},
		{ref: selection.root, object: selection.rootObject},
		{ref: selection.registered.objects[0].ref, object: selection.registered.objects[0].object},
	}
	if _, err := nativeTransferDocumentBudget(objects, budget); err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	var bodies [3]json.RawMessage
	for i, pin := range objects {
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
		if err != nil {
			return zero, fmt.Errorf("native reference document read: %w", err)
		}
		bodies[i] = raw
	}
	if err := color.VerifyWorkingImage(bodies[0]); err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	pixels, err := v2input.ResolveSemanticMaskPixels(selection.root, bodies[1], request.Intent.Reference, bodies[0], false)
	if err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	if pixels != selection.pixels {
		return zero, fmt.Errorf("selected pixels changed: %w", v2input.ErrConflict)
	}
	protected, err := v2input.ResolveSemanticMaskPixels(objects[2].ref, bodies[2], request.Intent.Reference, bodies[0], true)
	if err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	if protected != selection.registered.verified.Pixels || string(bodies[2]) != string(selection.registered.verified.Body) {
		return zero, fmt.Errorf("protected body changed: %w", v2input.ErrConflict)
	}
	_, afterSelection, err := s.verifyNativeInputSelectionSeparation(ctx, who, request, person.source, color)
	if err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	if !sameNativeInputSelection(ctx, afterSelection, selection) {
		return zero, fmt.Errorf("selection changed after IO: %w", v2input.ErrConflict)
	}
	afterPerson, err := s.prepareNativeInputReferencePersonMode(ctx, who, request, person.source.source, person.source.protection, color, person.person.report, person.originalPinned)
	if err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	if !sameNativeInputReferencePerson(ctx, afterPerson, person) {
		return zero, fmt.Errorf("reference person changed after IO: %w", v2input.ErrConflict)
	}
	if err := ctx.Err(); err != nil {
		return zero, fmt.Errorf("native reference document read: %w", err)
	}
	return nativeInputTransferReferenceDocuments{reference: bodies[0], selected: bodies[1], protection: bodies[2], person: person, selection: selection}, nil
}

// Caller combines transfer qualification, target authority and graph admission
// in this same transaction, and rolls back on every refusal.
func (s SQLTransferSourceReader) lockNativeInputTransferReference(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, documents nativeInputTransferReferenceDocuments, color TransferColorProfile, completed json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || len(documents.reference) == 0 || len(documents.selected) == 0 || len(documents.protection) == 0 {
		return v2input.ErrInvalid
	}
	refs := []v2input.Ref{request.Intent.Reference.Source, documents.selection.root, documents.selection.registered.verified.Root}
	for i, raw := range []json.RawMessage{documents.reference, documents.selected, documents.protection} {
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != refs[i].SHA256 {
			return v2input.ErrConflict
		}
	}
	if !sameRegisteredNativeInputProtection(ctx, documents.person.source.protection, documents.selection.registered) {
		return v2input.ErrConflict
	}
	if err := color.VerifyWorkingImage(documents.reference); err != nil {
		return err
	}
	selected, err := v2input.ResolveSemanticMaskPixels(documents.selection.root, documents.selected, request.Intent.Reference, documents.reference, false)
	if err != nil {
		return err
	}
	protected, err := v2input.ResolveSemanticMaskPixels(documents.selection.registered.verified.Root, documents.protection, request.Intent.Reference, documents.reference, true)
	if err != nil {
		return err
	}
	if selected != documents.selection.pixels || protected != documents.selection.registered.verified.Pixels || string(documents.protection) != string(documents.selection.registered.verified.Body) {
		return v2input.ErrConflict
	}
	if err := s.lockNativeInputReferencePerson(ctx, tx, who, request, documents.person, color, completed); err != nil {
		return err
	}
	return s.lockNativeInputSelectionSeparation(ctx, tx, who, request, documents.person.source, documents.selection, color, completed)
}
