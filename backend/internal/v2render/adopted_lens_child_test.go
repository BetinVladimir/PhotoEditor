package v2render

import "testing"

func TestAdoptedChildContractLimitsAndPaths(t *testing.T) {
	for _, contract := range []string{"WorkingImage", "ImageReport", "MaskRef", "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField"} {
		media, limit, ok := childObjectLimit(contract)
		if !ok || limit < 1 || media == "" {
			t.Fatal(contract)
		}
	}
	for _, contract := range []string{"Artifact", "Params", "Calibration", "ImageResult", "IdentityReport"} {
		if _, _, ok := childObjectLimit(contract); ok {
			t.Fatal("unscoped contract", contract)
		}
	}
	for _, path := range []string{"$/report", "$/working_image/pixels", "$/working_image/transform_chain/steps/0/inverse_field", "$/working_image/transform_chain/steps/*/inverse_field"} {
		if !adoptedChildPath.MatchString(path) {
			t.Fatal("valid path", path)
		}
	}
	for _, path := range []string{"$", "report", "$/../../report", "$/report?other=1", "$//report", "$/report/", "$/working_image/**/pixels", "$/working_image/step*/pixels"} {
		if adoptedChildPath.MatchString(path) {
			t.Fatal("invalid path", path)
		}
	}
}
