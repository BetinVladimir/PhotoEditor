package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
	"testing"
)

func TestTransferPreparationOwnsPolicyCollections(t *testing.T) {
	expected := v2qualification.Expected{RequiredChecks: []string{"quality"}, CalibrationSHAs: map[string]struct{}{"calibration": {}}, TestFixtureSHAs: map[string]struct{}{"fixture": {}}, EvidenceSHAs: map[string]struct{}{"evidence": {}}}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: "original"}
	original := TransferPreparation{Expected: expected, TargetProtection: []v2input.Ref{root}, PersonProtection: &TransferPersonProtection{Report: root, Expected: ownQualificationExpected(expected)}}
	owned := ownTransferPreparation(original)
	before := ownTransferPreparation(owned)
	original.Expected.RequiredChecks[0] = "changed"
	delete(original.Expected.CalibrationSHAs, "calibration")
	delete(original.Expected.TestFixtureSHAs, "fixture")
	delete(original.Expected.EvidenceSHAs, "evidence")
	original.TargetProtection[0] = v2input.Ref{}
	original.PersonProtection.Report = v2input.Ref{}
	original.PersonProtection.Expected.RequiredChecks[0] = "changed"
	delete(original.PersonProtection.Expected.CalibrationSHAs, "calibration")
	delete(original.PersonProtection.Expected.TestFixtureSHAs, "fixture")
	delete(original.PersonProtection.Expected.EvidenceSHAs, "evidence")
	if !reflect.DeepEqual(owned, before) {
		t.Fatal("deployment policy changed through aliased collections")
	}
	if ownTransferPreparation(TransferPreparation{}).PersonProtection != nil {
		t.Fatal("nil policy manufactured")
	}
	if ownQualificationExpected(v2qualification.Expected{}).EvidenceSHAs != nil {
		t.Fatal("nil set manufactured")
	}
}
