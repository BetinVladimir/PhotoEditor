package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// IdentityQualificationPin must be produced by the trusted qualification
// resolver. Supplying this value is not, by itself, proof of DBA authority.
type IdentityQualificationPin struct {
	PolicyID         uuid.UUID
	QualificationID  uuid.UUID
	Report           Ref
	Calibration      Ref
	ModelLockSHA     string
	RuntimeLockSHA   string
	DeviceProfileSHA string
	DatasetSHA       string
	Backend          string
}

func validIdentityQualificationPin(pin IdentityQualificationPin) bool {
	return pin.PolicyID != uuid.Nil && pin.QualificationID != uuid.Nil &&
		validRef(pin.Report) && validRef(pin.Calibration) &&
		pin.Report.ArtifactID != pin.Calibration.ArtifactID &&
		digestPattern.MatchString(pin.ModelLockSHA) &&
		digestPattern.MatchString(pin.RuntimeLockSHA) &&
		digestPattern.MatchString(pin.DeviceProfileSHA) &&
		digestPattern.MatchString(pin.DatasetSHA) &&
		(pin.Backend == "cpu" || pin.Backend == "cuda")
}

// CompileQualifiedIdentityCompareTask binds the physical resource estimate to
// the exact model/runtime/device policy the worker and adopter must recheck.
func CompileQualifiedIdentityCompareTask(ctx context.Context, workflow uuid.UUID, ordinal int,
	params IdentityCompareParams, paramsRef Ref, documents IdentityCompareDocuments,
	sizes []SemanticInputSize, costs SemanticQualifiedCosts, pin IdentityQualificationPin,
	ramLimit, vramLimit, scratchLimit, outputLimit int64) (FrozenIdentityCompareTask, error) {
	var zero FrozenIdentityCompareTask
	if !validIdentityQualificationPin(pin) || params.ModelLockSHA != pin.ModelLockSHA ||
		params.Calibration != pin.Calibration || pin.DatasetSHA == "" {
		return zero, ErrInvalid
	}
	frozen, err := CompileIdentityCompareTask(ctx, workflow, ordinal, pin.Backend,
		params, paramsRef, documents, pin.DatasetSHA, sizes, costs,
		ramLimit, vramLimit, scratchLimit, outputLimit)
	if err != nil {
		return zero, err
	}
	var profile map[string]json.RawMessage
	if json.Unmarshal(frozen.profile, &profile) != nil || profile == nil {
		return zero, ErrInvalid
	}
	add := func(key string, value any) error {
		raw, err := canonical.Marshal(value)
		if err != nil {
			return err
		}
		profile[key] = raw
		return nil
	}
	for _, item := range []struct {
		key   string
		value any
	}{
		{"model_lock_sha", pin.ModelLockSHA}, {"runtime_lock_sha", pin.RuntimeLockSHA},
		{"device_profile_sha", pin.DeviceProfileSHA}, {"dataset_sha", pin.DatasetSHA},
		{"qualification_policy_id", pin.PolicyID},
		{"qualification_id", pin.QualificationID},
		{"qualification_report", pin.Report},
		{"calibration", pin.Calibration}} {
		if err := add(item.key, item.value); err != nil {
			return zero, ErrInvalid
		}
	}
	bound, err := canonical.Marshal(profile)
	if err != nil || len(bound) > 1<<20 {
		return zero, ErrInvalid
	}
	frozen.profile = bound
	return frozen, ctx.Err()
}
