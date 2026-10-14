package v2portrait

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func TestLocalPolicyFingerprint(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	p := v2qualification.DeploymentLocks{Operation: "portrait.hair", Version: "1", Backend: "cpu", ModelLockSHA: strings.Repeat("a", 64), RuntimeLockSHA: strings.Repeat("b", 64), DeviceProfileSHA: strings.Repeat("c", 64), DatasetSHA: strings.Repeat("d", 64), IssuerID: id.String(), RequiredChecks: []string{"z", "a"}, TestFixtureSHAs: []string{strings.Repeat("e", 64)}, EvidenceSHAs: []string{strings.Repeat("f", 64)}, LicenseCleared: true}
	baseline, err := LocalPolicySHA(id, p)
	if err != nil || len(baseline) != 64 {
		t.Fatal(err)
	}
	if p.RequiredChecks[0] != "z" {
		t.Fatal("caller mutated")
	}
	p.RequiredChecks = []string{"a", "z"}
	p.CalibrationSHAs = []string{}
	same, err := LocalPolicySHA(id, p)
	if err != nil || same != baseline {
		t.Fatal("order/empty array mismatch", err)
	}
	issuerPolicy := p
	issuerPolicy.IssuerID = "ABCDEFAB-CDEF-ABCD-EFAB-CDEFABCDEFAB"
	upper, err := LocalPolicySHA(id, issuerPolicy)
	if err != nil {
		t.Fatal(err)
	}
	issuerPolicy.IssuerID = strings.ToLower(issuerPolicy.IssuerID)
	lower, err := LocalPolicySHA(id, issuerPolicy)
	if err != nil || upper != lower {
		t.Fatal("issuer UUID text changed canonical policy identity", err)
	}
	changed := p
	changed.RuntimeLockSHA = strings.Repeat("0", 64)
	other, err := LocalPolicySHA(id, changed)
	if err != nil || other == baseline {
		t.Fatal("runtime not bound")
	}
	for _, mutate := range []func(*v2qualification.DeploymentLocks){
		func(p *v2qualification.DeploymentLocks) { p.Backend = "cuda" },
		func(p *v2qualification.DeploymentLocks) { p.Operation = "semantic.transfer" },
		func(p *v2qualification.DeploymentLocks) { p.LicenseCleared = false },
		func(p *v2qualification.DeploymentLocks) { p.RequiredChecks = []string{"a", "a"} },
		func(p *v2qualification.DeploymentLocks) { p.TestFixtureSHAs = nil },
		func(p *v2qualification.DeploymentLocks) { p.CalibrationSHAs = p.TestFixtureSHAs },
	} {
		bad := p
		mutate(&bad)
		sha, err := LocalPolicySHA(id, bad)
		if err == nil || sha != "" {
			t.Fatal("invalid policy accepted")
		}
	}
	if sha, err := LocalPolicySHA(uuid.Nil, p); err == nil || sha != "" {
		t.Fatal("nil policy accepted")
	}
}
