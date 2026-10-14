package v2optical

import (
	"encoding/json"
	"os"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestNegativeQualificationOriginalEvidence(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-qualification.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Params      NegativeParams
		Calibration string
		Report      string
		Pin         NegativeQualificationPin
		Locks       v2qualification.DeploymentLocks
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	r, c := []byte(f.Report), []byte(f.Calibration)
	verified, err := VerifyNegativeQualificationEvidence(f.Pin, f.Locks, r, c, f.Params, f.Params.Base.CanvasHash)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := verified.Pin()
	if err != nil || retained != f.Pin {
		t.Fatal(err)
	}
	for _, mode := range []string{"report", "calibration", "license", "runtime", "binding", "icc"} {
		p, l, params := f.Pin, f.Locks, f.Params
		rr, cc := append([]byte(nil), r...), append([]byte(nil), c...)
		icc := params.Base.CanvasHash
		switch mode {
		case "report":
			rr[0] ^= 1
		case "calibration":
			cc[0] ^= 1
		case "license":
			l.LicenseCleared = false
		case "runtime":
			p.RuntimeLockSHA = p.ModelLockSHA
		case "binding":
			params.Calibration.ArtifactID = params.Base.Source.ArtifactID
		case "icc":
			icc = params.Calibration.SHA256
		}
		if _, err := VerifyNegativeQualificationEvidence(p, l, rr, cc, params, icc); err == nil {
			t.Fatal(mode)
		}
	}
	r[0] ^= 1
	c[0] ^= 1
	f.Locks.RequiredChecks[0] = "changed"
	if err := verified.Verify(); err != nil {
		t.Fatal("caller mutation", err)
	}
	if _, err := (VerifiedNegativeQualification{}).Pin(); err == nil {
		t.Fatal("zero accepted")
	}
}
