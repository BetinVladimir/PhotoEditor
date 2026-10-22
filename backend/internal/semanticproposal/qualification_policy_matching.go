package semanticproposal

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
	"sort"
)

func sameQualificationPolicyExpected(a, b v2qualification.Expected) bool {
	normalize := func(value v2qualification.Expected) ([]byte, error) {
		value = ownQualificationExpected(value)
		sort.Strings(value.RequiredChecks)
		for i := 1; i < len(value.RequiredChecks); i++ {
			if value.RequiredChecks[i] == value.RequiredChecks[i-1] {
				return nil, v2qualification.ErrUnqualified
			}
		}
		if value.TestFixtureSHAs == nil {
			value.TestFixtureSHAs = map[string]struct{}{}
		}
		if value.CalibrationSHAs == nil {
			value.CalibrationSHAs = map[string]struct{}{}
		}
		if value.EvidenceSHAs == nil {
			value.EvidenceSHAs = map[string]struct{}{}
		}
		return canonical.Marshal(value)
	}
	left, err := normalize(a)
	if err != nil {
		return false
	}
	right, err := normalize(b)
	return err == nil && string(left) == string(right)
}

// Checks every selected face/parser/person policy against the DBA-selected
// admission models. Does not replace original report/content or inventory gates.
func matchSelectedEvidencePolicies(policy qualificationPolicySnapshot, faces []facePreparationProof, parsers []faceParsePreparationProof, persons []personProtectionProof) error {
	selected, err := selectNativeQualificationPolicies(policy, []string{"face.landmarks", "face.detect", "face.parse", "mask.person"})
	if err != nil {
		return err
	}
	if len(faces) < 1 || len(faces) > 64 || len(parsers) < 1 || len(parsers) > 64 || len(persons) < 1 || len(persons) > 64 {
		return v2qualification.ErrUnqualified
	}
	match := func(body string, ops [2]string) bool {
		var values [2]v2qualification.Expected
		if json.Unmarshal([]byte(body), &values) != nil {
			return false
		}
		for i, value := range values {
			if value.Operation != ops[i] || !sameQualificationPolicyExpected(value, selected[ops[i]]) {
				return false
			}
		}
		return true
	}
	for _, proof := range faces {
		if !match(proof.expected, [2]string{"face.landmarks", "face.detect"}) {
			return v2qualification.ErrUnqualified
		}
	}
	for _, proof := range parsers {
		if !match(proof.evidence.expected, [2]string{"face.parse", "face.detect"}) {
			return v2qualification.ErrUnqualified
		}
	}
	for _, proof := range persons {
		var expected v2qualification.Expected
		if json.Unmarshal([]byte(proof.Expected), &expected) != nil || !sameQualificationPolicyExpected(expected, selected["mask.person"]) {
			return v2qualification.ErrUnqualified
		}
	}
	return nil
}

// Both execution policies are mandatory for the complete proposal contour.
// Matching policy does not verify report/calibration bytes or costs.
func matchProposalExecutionPolicies(policy qualificationPolicySnapshot, defect, transfer v2qualification.Expected) error {
	selected, err := selectNativeQualificationPolicies(policy, []string{"defects.propose", "semantic.transfer"})
	if err != nil {
		return err
	}
	if !sameQualificationPolicyExpected(defect, selected["defects.propose"]) || !sameQualificationPolicyExpected(transfer, selected["semantic.transfer"]) {
		return v2qualification.ErrUnqualified
	}
	return nil
}
