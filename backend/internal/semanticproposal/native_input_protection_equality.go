package semanticproposal

import (
	"context"
	"reflect"
)

// Published plans contain executable CaptureClosure.Open functions. Reflect on
// those closures is always unequal; compare their verified semantic plan seals
// and complete shell instead. Physical result/document/object pins remain exact.
// This is equality only, never producer authority or parent admission.
func sameRegisteredNativeInputProtection(ctx context.Context, a, b registeredNativeInputProtection) bool {
	if ctx.Err() != nil {
		return false
	}
	x, y := a.published.preparation, b.published.preparation
	if verifyNativeDefectDraftSeal(ctx, x.plan) != nil || verifyNativeDefectDraftSeal(ctx, y.plan) != nil {
		return false
	}
	return a.published.root == b.published.root && a.objects == b.objects &&
		reflect.DeepEqual(a.verified, b.verified) && x.document == y.document && x.body == y.body && x.pixels == y.pixels &&
		reflect.DeepEqual(x.result, y.result) && x.plan.seal == y.plan.seal && x.plan.key == y.plan.key && reflect.DeepEqual(x.plan.shell, y.plan.shell)
}
