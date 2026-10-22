package facequalification

import "photoeditor/backend/internal/v2qualification"

func ownExpected(value v2qualification.Expected) v2qualification.Expected {
	if value.MPSResources != nil {
		resources := *value.MPSResources
		value.MPSResources = &resources
	}
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
