package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// RestorationQualificationPin is specific to portrait.glare/image.reflection.
// Its shape is distinct from the open-eyes donor qualification.
type RestorationQualificationPin struct {
	Operation        string      `json:"operation"`
	OperationVersion string      `json:"operation_version"`
	PolicyID         uuid.UUID   `json:"policy_id"`
	QualificationID  uuid.UUID   `json:"qualification_id"`
	PolicySHA        string      `json:"policy_sha"`
	Report           v2input.Ref `json:"report"`
	Calibration      v2input.Ref `json:"calibration"`
	ModelLockSHA     string      `json:"model_lock_sha"`
	RuntimeLockSHA   string      `json:"runtime_lock_sha"`
	DeviceProfileSHA string      `json:"device_profile_sha"`
	DatasetSHA       string      `json:"dataset_sha"`
	IssuerID         uuid.UUID   `json:"issuer_id"`
	Backend          string      `json:"backend"`
}

func (p RestorationQualificationPin) Compile() ([]byte, error) {
	if (p.Operation != "portrait.glare" && p.Operation != "image.reflection") ||
		p.OperationVersion != "1" || (p.Backend != "cpu" && p.Backend != "cuda") ||
		p.PolicyID == uuid.Nil || p.QualificationID == uuid.Nil || p.IssuerID == uuid.Nil ||
		p.Report.ArtifactID == uuid.Nil || p.Calibration.ArtifactID == uuid.Nil ||
		p.Report.ArtifactID == p.Calibration.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	for _, sha := range []string{p.PolicySHA, p.Report.SHA256, p.Calibration.SHA256,
		p.ModelLockSHA, p.RuntimeLockSHA, p.DeviceProfileSHA, p.DatasetSHA} {
		if _, err := domain.ParseDigest(sha); err != nil {
			return nil, v2input.ErrInvalid
		}
	}
	return canonical.Marshal(p)
}

// LockRestorationQualification checks the live tenant/project/editor catalog
// authority in the same scheduler transaction that owns draft publication.
// Physical report bytes and model files still require independent verification.
func LockRestorationQualification(ctx context.Context, tx pgx.Tx,
	who auth.Identity, tenant, project uuid.UUID, pin RestorationQualificationPin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	body, err := pin.Compile()
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err := tx.QueryRow(ctx,
		"SELECT photo.lock_v28_restoration_qualification($1,$2,$3,$4,$5,$6::jsonb)",
		tenant, project, who.UserID, who.SessionID, pin.Operation, string(body)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}

// CompilePinnedRestorationProfile binds a previously selected qualification to
// the exact Params and admitted GPU-class resource profile. Catalog authority,
// model-file verification and live consent are checked separately at admission.
func CompilePinnedRestorationProfile(operation string, paramsRaw, profileRaw []byte,
	pin RestorationQualificationPin) (json.RawMessage, error) {
	if len(paramsRaw) < 2 || len(paramsRaw) > 65536 ||
		len(profileRaw) < 2 || len(profileRaw) > 65536 || pin.Operation != operation {
		return nil, v2input.ErrInvalid
	}
	pinRaw, err := pin.Compile()
	if err != nil {
		return nil, err
	}
	var params struct {
		SchemaVersion int         `json:"schema_version"`
		ModelLockSHA  string      `json:"model_lock_sha"`
		Calibration   v2input.Ref `json:"calibration"`
	}
	if err := json.Unmarshal(paramsRaw, &params); err != nil ||
		params.SchemaVersion != 1 || params.ModelLockSHA != pin.ModelLockSHA ||
		params.Calibration != pin.Calibration {
		return nil, v2input.ErrInvalid
	}
	var paramsFields map[string]json.RawMessage
	if json.Unmarshal(paramsRaw, &paramsFields) != nil || paramsFields == nil {
		return nil, v2input.ErrInvalid
	}
	canonicalParams, err := canonical.Marshal(paramsFields)
	if err != nil || !bytes.Equal(canonicalParams, paramsRaw) {
		return nil, v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(profileRaw, &fields); err != nil || fields == nil {
		return nil, v2input.ErrInvalid
	}
	canonicalProfile, err := canonical.Marshal(fields)
	if err != nil || !bytes.Equal(canonicalProfile, profileRaw) {
		return nil, v2input.ErrInvalid
	}
	if _, exists := fields["restoration_qualification"]; exists {
		return nil, v2input.ErrInvalid
	}
	var backend, renderer string
	var resources LocalResourceEstimate
	if json.Unmarshal(fields["backend"], &backend) != nil || backend != pin.Backend ||
		json.Unmarshal(fields["renderer_version"], &renderer) != nil || renderer != "v2.2" ||
		json.Unmarshal(fields["resource_estimate"], &resources) != nil ||
		resources.PixelCount < 1 || resources.PixelCount > 200000000 ||
		resources.RAMBytes < 2*1024*1024*1024+resources.PixelCount*256 ||
		resources.ScratchBytes < 1 || resources.OutputBytes < 1 ||
		resources.RAMBytes > 9007199254740991 || resources.ScratchBytes > 9007199254740991 ||
		resources.OutputBytes > 9007199254740991 {
		return nil, v2input.ErrInvalid
	}
	fields["restoration_qualification"] = pinRaw
	fields["restoration_runtime_sha"], err = canonical.Marshal(pin.RuntimeLockSHA)
	if err != nil {
		return nil, err
	}
	return canonical.Marshal(fields)
}
