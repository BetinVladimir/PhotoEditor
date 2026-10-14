package v2optical

import (
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type LocatedMultiQualification struct {
	verified VerifiedMultiQualification
	objects  [2]PerspectiveQualificationObject
}

// Objects returns owned original metadata for backend durable recovery.
func (v LocatedMultiQualification) Objects() ([]PerspectiveQualificationObject, error) {
	if _, err := v.Qualification(); err != nil {
		return nil, err
	}
	return append([]PerspectiveQualificationObject(nil), v.objects[:]...), nil
}

// BindMultiQualificationObjects joins exact verified bytes with authorized
// catalog locations. The object reader must have read these original versions;
// a caller supplying metadata without catalog authority cannot establish scope.
func BindMultiQualificationObjects(verified VerifiedMultiQualification, bucket string, report, calibration objectstore.Object) (LocatedMultiQualification, error) {
	var zero LocatedMultiQualification
	pin, err := verified.Pin()
	if err != nil {
		return zero, err
	}
	if bucket == "" || len(bucket) > 255 {
		return zero, v2input.ErrInvalid
	}
	refs := []v2input.Ref{pin.Calibration, pin.Report}
	objs := []objectstore.Object{calibration, report}
	bodies := [][]byte{verified.calibrationRaw, verified.reportRaw}
	labels := []string{"Calibration", "Report"}
	value := LocatedMultiQualification{verified: verified}
	for i, obj := range objs {
		if obj.SHA256 != refs[i].SHA256 || obj.Bytes != int64(len(bodies[i])) || obj.ContentType != "application/json" || obj.Key == "" || len(obj.Key) > 1024 || obj.VersionID == "" || obj.VersionID == "null" || len(obj.VersionID) > 1024 {
			return zero, v2input.ErrInvalid
		}
		value.objects[i] = PerspectiveQualificationObject{Contract: labels[i], ArtifactID: refs[i].ArtifactID, SHA256: refs[i].SHA256, Bucket: bucket, Key: obj.Key, Version: obj.VersionID, Bytes: obj.Bytes}
	}
	return value, nil
}

func (v LocatedMultiQualification) Qualification() (VerifiedMultiQualification, error) {
	if err := v.verified.Verify(); err != nil {
		return VerifiedMultiQualification{}, err
	}
	if v.objects[0].ArtifactID != v.verified.pin.Calibration.ArtifactID || v.objects[1].ArtifactID != v.verified.pin.Report.ArtifactID {
		return VerifiedMultiQualification{}, v2input.ErrInvalid
	}
	return v.verified, nil
}
