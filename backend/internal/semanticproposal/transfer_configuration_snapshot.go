package semanticproposal

import (
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// ownQualificationExpected detaches mutable policy collections before callbacks.
// Deployment configuration must not be concurrently written during the copy.
func ownQualificationExpected(value v2qualification.Expected) v2qualification.Expected {
	value.RequiredChecks = append([]string(nil), value.RequiredChecks...)
	value.CalibrationSHAs = ownDigestSet(value.CalibrationSHAs)
	value.TestFixtureSHAs = ownDigestSet(value.TestFixtureSHAs)
	value.EvidenceSHAs = ownDigestSet(value.EvidenceSHAs)
	return value
}

func ownDigestSet(value map[string]struct{}) map[string]struct{} {
	if value == nil {
		return nil
	}
	result := make(map[string]struct{}, len(value))
	for key := range value {
		result[key] = struct{}{}
	}
	return result
}

// ownTransferPreparation keeps one policy for validation, object IO and compile.
func ownTransferPreparation(value TransferPreparation) TransferPreparation {
	value.Expected = ownQualificationExpected(value.Expected)
	value.TargetProtection = append([]v2input.Ref(nil), value.TargetProtection...)
	if value.PersonProtection != nil {
		policy := *value.PersonProtection
		policy.Expected = ownQualificationExpected(policy.Expected)
		value.PersonProtection = &policy
	}
	return value
}
