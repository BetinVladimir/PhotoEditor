package v2optical

import (
	"photoeditor/backend/internal/v2input"
	"reflect"
)

// Optional exact locator binding for producer orchestration. SQL authority is
// still mandatory; located content by itself grants no execution capability.
func validateMultiLocated(qualified VerifiedMultiQualification, located []LocatedMultiQualification) error {
	if len(located) > 1 {
		return v2input.ErrInvalid
	}
	if len(located) == 0 {
		return nil
	}
	verified, err := located[0].Qualification()
	if err != nil {
		return err
	}
	if verified.operation != qualified.operation || verified.pin != qualified.pin || verified.paramsRaw != qualified.paramsRaw || !reflect.DeepEqual(verified.sourceRaw, qualified.sourceRaw) {
		return v2input.ErrInvalid
	}
	return nil
}
