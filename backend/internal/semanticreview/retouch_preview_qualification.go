package semanticreview

import "photoeditor/backend/internal/v2input"

// RetouchQualificationPin describes the existing CPU BeautyFilterV1 pipeline.
// Structural validation grants no authority: exact own-operation report and
// current SQL policy gates are mandatory before dispatch and every worker IO.
type RetouchQualificationPin v2input.SemanticQualificationPin

func validRetouchQualification(p RetouchQualificationPin) bool {
	return p.Backend == "cpu" && validExposureQualification(ExposureQualificationPin(p))
}
