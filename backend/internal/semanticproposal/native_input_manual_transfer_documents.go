package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeInputManualTransferReferenceDocuments struct {
	reference, selected, protection json.RawMessage
	person                          nativeInputManualReferencePerson
	selection                       nativeInputManualSelectionSeparationProof
}

// Creates compiler documents only after independent person coverage and
// nonempty zero-overlap selection, retaining the original native-input authority.
func (s SQLTransferSourceReader) readNativeInputManualTransferReferenceDocuments(ctx context.Context, who auth.Identity, request Request, person nativeInputManualReferencePerson, color TransferColorProfile, budget int64) (nativeInputManualTransferReferenceDocuments, error) {
	var zero nativeInputManualTransferReferenceDocuments
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || budget <= 0 || budget > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	current, err := s.prepareNativeInputManualReferencePerson(ctx, who, request, person.source.source, person.source.protection, color, person.person.report)
	if err != nil {
		return zero, err
	}
	if !sameNativeInputManualReferencePerson(ctx, current, person) {
		return zero, v2input.ErrConflict
	}
	_, selection, err := s.verifyNativeInputManualSelectionSeparation(ctx, who, request, person.source, color)
	if err != nil {
		return zero, err
	}
	if _, err := s.marshalNativeInputManualSelectionPins(who, request, selection); err != nil {
		return zero, err
	}
	pins, err := decodeSourcePreparationPins(person.source.source)
	if err != nil {
		return zero, err
	}
	objects := []personEvidenceObject{
		{ref: request.Intent.Reference.Source, object: pins[0].Object},
		{ref: selection.root, object: selection.rootObject},
		{ref: selection.registered.objects[0].ref, object: selection.registered.objects[0].object},
	}
	if _, err := nativeTransferDocumentBudget(objects, budget); err != nil {
		return zero, err
	}
	var bodies [3]json.RawMessage
	for i, pin := range objects {
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
		if err != nil {
			return zero, err
		}
		bodies[i] = raw
	}
	if err := color.VerifyWorkingImage(bodies[0]); err != nil {
		return zero, err
	}
	pixels, err := v2input.ResolveSemanticMaskPixels(selection.root, bodies[1], request.Intent.Reference, bodies[0], false)
	if err != nil {
		return zero, err
	}
	if pixels != selection.pixels {
		return zero, v2input.ErrConflict
	}
	protected, err := v2input.ResolveSemanticMaskPixels(objects[2].ref, bodies[2], request.Intent.Reference, bodies[0], true)
	if err != nil {
		return zero, err
	}
	if protected != selection.registered.verified.Pixels || string(bodies[2]) != string(selection.registered.verified.Body) {
		return zero, v2input.ErrConflict
	}
	_, afterSelection, err := s.verifyNativeInputManualSelectionSeparation(ctx, who, request, person.source, color)
	if err != nil {
		return zero, err
	}
	if !sameNativeInputManualSelection(ctx, afterSelection, selection) {
		return zero, v2input.ErrConflict
	}
	afterPerson, err := s.prepareNativeInputManualReferencePerson(ctx, who, request, person.source.source, person.source.protection, color, person.person.report)
	if err != nil {
		return zero, err
	}
	if !sameNativeInputManualReferencePerson(ctx, afterPerson, person) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeInputManualTransferReferenceDocuments{reference: bodies[0], selected: bodies[1], protection: bodies[2], person: person, selection: selection}, nil
}

// Caller combines transfer qualification, target authority and graph admission
// in this same transaction, and rolls back on every refusal.
func (s SQLTransferSourceReader) lockNativeInputManualTransferReference(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, documents nativeInputManualTransferReferenceDocuments, color TransferColorProfile, completed json.RawMessage) error {
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
	if !sameRegisteredNativeInputManualProtection(ctx, documents.person.source.protection, documents.selection.registered) {
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
	if err := s.lockNativeInputManualReferencePerson(ctx, tx, who, request, documents.person, color, completed); err != nil {
		return err
	}
	return s.lockNativeInputManualSelectionSeparation(ctx, tx, who, request, documents.person.source, documents.selection, color, completed)
}
