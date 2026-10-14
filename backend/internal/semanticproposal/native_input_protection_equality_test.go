package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestRegisteredNativeProtectionEqualityRetainsSemanticPins(t *testing.T) {
	base := registeredNativeInputProtection{}
	var err error
	base.published.preparation.plan.seal, err = nativeDefectDraftDigest(base.published.preparation.plan)
	if err != nil {
		t.Fatal(err)
	}
	if !sameRegisteredNativeInputProtection(context.Background(), base, base) {
		t.Fatal("identical sealed value refused")
	}
	for _, kind := range []string{"root", "pixels", "body", "document", "object", "verified", "result", "plan-seal", "plan-key", "plan-shell"} {
		changed := base
		switch kind {
		case "root":
			changed.published.root.ArtifactID = uuid.New()
		case "pixels":
			changed.published.preparation.pixels.ArtifactID = uuid.New()
		case "body":
			changed.published.preparation.body = "substitute"
		case "document":
			changed.published.preparation.document = uuid.New()
		case "object":
			changed.objects[1].object.VersionID = "substitute"
		case "verified":
			changed.verified.Body = []byte("substitute")
		case "result":
			changed.published.preparation.result.evidence.Report = []byte("substitute")
		case "plan-seal":
			changed.published.preparation.plan.seal = "substitute"
		case "plan-key":
			changed.published.preparation.plan.key = "substitute"
		case "plan-shell":
			changed.published.preparation.plan.shell.Run = uuid.New()
		}
		if sameRegisteredNativeInputProtection(context.Background(), base, changed) {
			t.Fatal("changed authority accepted", kind)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sameRegisteredNativeInputProtection(ctx, base, base) {
		t.Fatal("cancelled comparison accepted")
	}
}
