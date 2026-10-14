package v2input

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func TestSemanticPolicyHashOwnsChecksAndBindsAcceptanceRules(t *testing.T) {
	set := func(c string) map[string]struct{} { return map[string]struct{}{strings.Repeat(c, 64): {}} }
	e := v2qualification.Expected{Operation: "semantic.transfer", Version: "1", Backend: "cuda", ModelLockSHA: strings.Repeat("a", 64), RuntimeLockSHA: strings.Repeat("b", 64), DeviceProfileSHA: strings.Repeat("c", 64), DatasetSHA: strings.Repeat("d", 64), IssuerID: uuid.New(), RequiredChecks: []string{"geometry", "quality"}, TestFixtureSHAs: set("e"), CalibrationSHAs: set("f"), EvidenceSHAs: set("0"), LicenseCleared: true}
	policy := uuid.New()
	first, err := SemanticQualificationPolicyHash(policy, e)
	if err != nil {
		t.Fatal(err)
	}
	e.RequiredChecks = []string{"quality", "geometry"}
	reordered, err := SemanticQualificationPolicyHash(policy, e)
	if err != nil || first != reordered || e.RequiredChecks[0] != "quality" {
		t.Fatal("unstable/borrowed check ordering", err)
	}
	e.RequiredChecks = []string{"quality", "identity"}
	changed, err := SemanticQualificationPolicyHash(policy, e)
	if err != nil || changed == first {
		t.Fatal("changed checks retained pin", err)
	}
	e.RequiredChecks = []string{"quality", "quality"}
	if _, err := SemanticQualificationPolicyHash(policy, e); err == nil {
		t.Fatal("duplicate required checks accepted")
	}
	e.RequiredChecks = []string{"quality"}
	e.LicenseCleared = false
	if _, err := SemanticQualificationPolicyHash(policy, e); err == nil {
		t.Fatal("uncleared license accepted")
	}
}
