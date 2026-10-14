package semanticproposal

import (
	"context"
	"reflect"
)

// Typed semantic comparison: executable closure identity is not evidence.
// Each nested registered proof keeps its verified seal/shell and exact pins.
func sameRegisteredNativeInputManualProtection(ctx context.Context, a, b registeredNativeInputManualInitialProtection) bool {
	if ctx.Err() != nil {
		return false
	}
	x, y := a.published.preparation, b.published.preparation
	if verifyNativeInputManualInitialDraftSeal(ctx, x.plan) != nil || verifyNativeInputManualInitialDraftSeal(ctx, y.plan) != nil {
		return false
	}
	return a.published.root == b.published.root && a.objects == b.objects && reflect.DeepEqual(a.verified, b.verified) && x.document == y.document && x.body == y.body && x.pixels == y.pixels && reflect.DeepEqual(x.result, y.result) && x.plan.seal == y.plan.seal && x.plan.key == y.plan.key && reflect.DeepEqual(x.plan.shell, y.plan.shell)
}
func sameNativeInputReferencePerson(ctx context.Context, a, b nativeInputReferencePerson) bool {
	if !sameRegisteredNativeInputProtection(ctx, a.source.protection, b.source.protection) || !sameRegisteredNativeInputProtection(ctx, a.person.registered, b.person.registered) {
		return false
	}
	a.source.protection = registeredNativeInputProtection{}
	b.source.protection = registeredNativeInputProtection{}
	a.person.registered = registeredNativeInputProtection{}
	b.person.registered = registeredNativeInputProtection{}
	return reflect.DeepEqual(a, b)
}
func sameNativeInputSelection(ctx context.Context, a, b nativeInputSelectionSeparationProof) bool {
	if !sameRegisteredNativeInputProtection(ctx, a.registered, b.registered) {
		return false
	}
	a.registered = registeredNativeInputProtection{}
	b.registered = registeredNativeInputProtection{}
	return reflect.DeepEqual(a, b)
}
func sameNativeInputTransferDocuments(ctx context.Context, a, b nativeInputTransferReferenceDocuments) bool {
	if !sameNativeInputReferencePerson(ctx, a.person, b.person) || !sameNativeInputSelection(ctx, a.selection, b.selection) {
		return false
	}
	a.person = nativeInputReferencePerson{}
	b.person = nativeInputReferencePerson{}
	a.selection = nativeInputSelectionSeparationProof{}
	b.selection = nativeInputSelectionSeparationProof{}
	return reflect.DeepEqual(a, b)
}
func sameNativeInputManualReferencePerson(ctx context.Context, a, b nativeInputManualReferencePerson) bool {
	if !sameRegisteredNativeInputManualProtection(ctx, a.source.protection, b.source.protection) || !sameRegisteredNativeInputManualProtection(ctx, a.person.registered, b.person.registered) {
		return false
	}
	a.source.protection = registeredNativeInputManualInitialProtection{}
	b.source.protection = registeredNativeInputManualInitialProtection{}
	a.person.registered = registeredNativeInputManualInitialProtection{}
	b.person.registered = registeredNativeInputManualInitialProtection{}
	return reflect.DeepEqual(a, b)
}
func sameNativeInputManualSelection(ctx context.Context, a, b nativeInputManualSelectionSeparationProof) bool {
	if !sameRegisteredNativeInputManualProtection(ctx, a.registered, b.registered) {
		return false
	}
	a.registered = registeredNativeInputManualInitialProtection{}
	b.registered = registeredNativeInputManualInitialProtection{}
	return reflect.DeepEqual(a, b)
}
func sameNativeInputManualTransferDocuments(ctx context.Context, a, b nativeInputManualTransferReferenceDocuments) bool {
	if !sameNativeInputManualReferencePerson(ctx, a.person, b.person) || !sameNativeInputManualSelection(ctx, a.selection, b.selection) {
		return false
	}
	a.person = nativeInputManualReferencePerson{}
	b.person = nativeInputManualReferencePerson{}
	a.selection = nativeInputManualSelectionSeparationProof{}
	b.selection = nativeInputManualSelectionSeparationProof{}
	return reflect.DeepEqual(a, b)
}

// Compare independently restored original producers by verified semantic seals
// and complete persisted shells; no executable closure pointer is authoritative.
func sameNativeInputFaceProducers(ctx context.Context, a, b nativeInputRestoredFaceProducers) bool {
	for _, p := range []nativeInputRestoredFaceProducers{a, b} {
		if verifyDetectorPlanSeal(ctx, p.detector) != nil || verifyNativeFacePlanSeal(ctx, p.landmarks) != nil || verifyNativeFacePlanSeal(ctx, p.parser) != nil || verifyNativeSkinPlanSeal(ctx, p.skin) != nil {
			return false
		}
	}
	return a.detector.seal == b.detector.seal && a.detector.key == b.detector.key && reflect.DeepEqual(a.detector.shell, b.detector.shell) && a.landmarks.seal == b.landmarks.seal && a.landmarks.key == b.landmarks.key && reflect.DeepEqual(a.landmarks.shell, b.landmarks.shell) && a.parser.seal == b.parser.seal && a.parser.key == b.parser.key && reflect.DeepEqual(a.parser.shell, b.parser.shell) && a.skin.seal == b.skin.seal && a.skin.key == b.skin.key && reflect.DeepEqual(a.skin.shell, b.skin.shell)
}

func sameNativeInputManualInitialStage(ctx context.Context, a, b nativeInputManualInitialStage) bool {
	if !sameNativeInputFaceProducers(ctx, a.dependencies.dependencies.producers, b.dependencies.dependencies.producers) {
		return false
	}
	a.dependencies.dependencies.producers = nativeInputRestoredFaceProducers{}
	b.dependencies.dependencies.producers = nativeInputRestoredFaceProducers{}
	return reflect.DeepEqual(a, b)
}
