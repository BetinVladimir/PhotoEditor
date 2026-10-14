package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

// Pure equality checks are not admission or physical authority evidence.
func TestNativeInputNestedEqualityRetainsAllOtherFields(t *testing.T) {
	ctx := context.Background()
	registered := registeredNativeInputProtection{}
	var err error
	registered.published.preparation.plan.seal, err = nativeDefectDraftDigest(registered.published.preparation.plan)
	if err != nil {
		t.Fatal(err)
	}
	base := nativeInputTransferReferenceDocuments{person: nativeInputReferencePerson{source: nativeInputSourceContinuation{protection: registered}, person: nativeInputPersonProtectionProof{registered: registered}}, selection: nativeInputSelectionSeparationProof{registered: registered}}
	if !sameNativeInputTransferDocuments(ctx, base, base) {
		t.Fatal("identical nested data refused")
	}
	cases := map[string]func(*nativeInputTransferReferenceDocuments){
		"reference bytes":  func(p *nativeInputTransferReferenceDocuments) { p.reference = []byte("other") },
		"selected bytes":   func(p *nativeInputTransferReferenceDocuments) { p.selected = []byte("other") },
		"protection bytes": func(p *nativeInputTransferReferenceDocuments) { p.protection = []byte("other") },
		"source scope": func(p *nativeInputTransferReferenceDocuments) {
			p.person.source.source.binding.AssetVersionID = uuid.New()
		},
		"policy":                  func(p *nativeInputTransferReferenceDocuments) { p.person.policy.body = "other" },
		"person root":             func(p *nativeInputTransferReferenceDocuments) { p.person.person.root.ArtifactID = uuid.New() },
		"person pixels":           func(p *nativeInputTransferReferenceDocuments) { p.person.person.pixels.ArtifactID = uuid.New() },
		"person report":           func(p *nativeInputTransferReferenceDocuments) { p.person.person.report.ArtifactID = uuid.New() },
		"person expected":         func(p *nativeInputTransferReferenceDocuments) { p.person.person.expected = "other" },
		"person evidence":         func(p *nativeInputTransferReferenceDocuments) { p.person.person.evidence = "other" },
		"person qualification":    func(p *nativeInputTransferReferenceDocuments) { p.person.person.qualification = "other" },
		"person count":            func(p *nativeInputTransferReferenceDocuments) { p.person.person.count++ },
		"selection root":          func(p *nativeInputTransferReferenceDocuments) { p.selection.root.ArtifactID = uuid.New() },
		"selection pixels":        func(p *nativeInputTransferReferenceDocuments) { p.selection.pixels.ArtifactID = uuid.New() },
		"selection root version":  func(p *nativeInputTransferReferenceDocuments) { p.selection.rootObject.VersionID = "other" },
		"selection pixel version": func(p *nativeInputTransferReferenceDocuments) { p.selection.pixelsObject.VersionID = "other" },
		"selection count":         func(p *nativeInputTransferReferenceDocuments) { p.selection.count++ },
		"source registered": func(p *nativeInputTransferReferenceDocuments) {
			p.person.source.protection.published.root.ArtifactID = uuid.New()
		},
		"person registered": func(p *nativeInputTransferReferenceDocuments) {
			p.person.person.registered.objects[1].object.VersionID = "other"
		},
		"selection registered": func(p *nativeInputTransferReferenceDocuments) {
			p.selection.registered.published.preparation.body = "other"
		},
	}
	for name, mutate := range cases {
		changed := base
		mutate(&changed)
		if sameNativeInputTransferDocuments(ctx, base, changed) {
			t.Fatal("changed proof accepted", name)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if sameNativeInputTransferDocuments(cancelled, base, base) {
		t.Fatal("cancelled equality accepted")
	}
}
func TestNativeInputManualNestedEqualityRetainsAllOtherFields(t *testing.T) {
	ctx := context.Background()
	registered := registeredNativeInputManualInitialProtection{}
	var err error
	registered.published.preparation.plan.seal, err = nativeInputManualInitialDraftDigest(registered.published.preparation.plan)
	if err != nil {
		t.Fatal(err)
	}
	base := nativeInputManualTransferReferenceDocuments{person: nativeInputManualReferencePerson{source: nativeInputManualSourceContinuation{protection: registered}, person: nativeInputManualPersonProtectionProof{registered: registered}}, selection: nativeInputManualSelectionSeparationProof{registered: registered}}
	if !sameNativeInputManualTransferDocuments(ctx, base, base) {
		t.Fatal("identical nested data refused")
	}
	cases := map[string]func(*nativeInputManualTransferReferenceDocuments){
		"reference bytes":  func(p *nativeInputManualTransferReferenceDocuments) { p.reference = []byte("other") },
		"selected bytes":   func(p *nativeInputManualTransferReferenceDocuments) { p.selected = []byte("other") },
		"protection bytes": func(p *nativeInputManualTransferReferenceDocuments) { p.protection = []byte("other") },
		"source scope": func(p *nativeInputManualTransferReferenceDocuments) {
			p.person.source.source.binding.AssetVersionID = uuid.New()
		},
		"policy":                  func(p *nativeInputManualTransferReferenceDocuments) { p.person.policy.body = "other" },
		"person root":             func(p *nativeInputManualTransferReferenceDocuments) { p.person.person.root.ArtifactID = uuid.New() },
		"person pixels":           func(p *nativeInputManualTransferReferenceDocuments) { p.person.person.pixels.ArtifactID = uuid.New() },
		"person report":           func(p *nativeInputManualTransferReferenceDocuments) { p.person.person.report.ArtifactID = uuid.New() },
		"person expected":         func(p *nativeInputManualTransferReferenceDocuments) { p.person.person.expected = "other" },
		"person evidence":         func(p *nativeInputManualTransferReferenceDocuments) { p.person.person.evidence = "other" },
		"person qualification":    func(p *nativeInputManualTransferReferenceDocuments) { p.person.person.qualification = "other" },
		"person count":            func(p *nativeInputManualTransferReferenceDocuments) { p.person.person.count++ },
		"selection root":          func(p *nativeInputManualTransferReferenceDocuments) { p.selection.root.ArtifactID = uuid.New() },
		"selection pixels":        func(p *nativeInputManualTransferReferenceDocuments) { p.selection.pixels.ArtifactID = uuid.New() },
		"selection root version":  func(p *nativeInputManualTransferReferenceDocuments) { p.selection.rootObject.VersionID = "other" },
		"selection pixel version": func(p *nativeInputManualTransferReferenceDocuments) { p.selection.pixelsObject.VersionID = "other" },
		"selection count":         func(p *nativeInputManualTransferReferenceDocuments) { p.selection.count++ },
		"source registered": func(p *nativeInputManualTransferReferenceDocuments) {
			p.person.source.protection.published.root.ArtifactID = uuid.New()
		},
		"person registered": func(p *nativeInputManualTransferReferenceDocuments) {
			p.person.person.registered.objects[1].object.VersionID = "other"
		},
		"selection registered": func(p *nativeInputManualTransferReferenceDocuments) {
			p.selection.registered.published.preparation.body = "other"
		},
	}
	for name, mutate := range cases {
		changed := base
		mutate(&changed)
		if sameNativeInputManualTransferDocuments(ctx, base, changed) {
			t.Fatal("changed proof accepted", name)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if sameNativeInputManualTransferDocuments(cancelled, base, base) {
		t.Fatal("cancelled equality accepted")
	}
}
