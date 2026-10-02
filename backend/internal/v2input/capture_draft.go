package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
)

type CaptureDraftConfig struct {
	ProjectID uuid.UUID `json:"project_id"`
	Capture   struct {
		JobID          uuid.UUID            `json:"job_id"`
		Policy         json.RawMessage      `json:"policy"`
		PolicySHA      string               `json:"policy_sha"`
		SnapshotDigest string               `json:"snapshot_digest"`
		Params         CapturePrepareParams `json:"params"`
		ParamsJCS      string               `json:"params_jcs"`
		ParamsSHA      string               `json:"params_sha"`
		Profile        json.RawMessage      `json:"profile"`
	} `json:"v24_capture"`
}

func (s SQLCaptureAssessmentStore) BeginDraft(ctx context.Context, l CaptureAssessmentLease,
	binding SessionVersionBinding, modelLockSHA string, profile json.RawMessage, reservedBytes int64) (CaptureDraftConfig, error) {
	if s.DB == nil || !digestPattern.MatchString(modelLockSHA) || !json.Valid(profile) || reservedBytes < 1 || reservedBytes > 9007199254740991 {
		return CaptureDraftConfig{}, ErrInvalid
	}
	_, _, snapshotSHA, err := CompileCaptureSnapshot(l, binding)
	if err != nil {
		return CaptureDraftConfig{}, err
	}
	var policy CapturePolicy
	if json.Unmarshal(l.Policy, &policy) != nil || policy.SchemaVersion != 1 || policy.MaxQueueAgeSeconds != 30 || !validRef(policy.Calibration) {
		return CaptureDraftConfig{}, ErrInvalid
	}
	policySHA, err := canonical.Hash(l.Policy)
	if err != nil {
		return CaptureDraftConfig{}, err
	}
	config := CaptureDraftConfig{ProjectID: l.ProjectID}
	config.Capture.JobID = l.JobID
	config.Capture.Policy = l.Policy
	config.Capture.PolicySHA = policySHA
	config.Capture.SnapshotDigest = snapshotSHA
	config.Capture.Profile = profile
	config.Capture.Params = CapturePrepareParams{1, binding, policy.Calibration, modelLockSHA}
	paramsBody, err := canonical.Marshal(config.Capture.Params)
	if err != nil {
		return CaptureDraftConfig{}, err
	}
	config.Capture.ParamsJCS = string(paramsBody)
	config.Capture.ParamsSHA, err = canonical.Hash(config.Capture.Params)
	if err != nil {
		return CaptureDraftConfig{}, err
	}
	body, err := canonical.Marshal(config)
	if err != nil {
		return CaptureDraftConfig{}, err
	}
	run, workflow, _, err := CaptureDraftIDs(l.JobID)
	if err != nil {
		return CaptureDraftConfig{}, err
	}
	var begun bool
	err = s.DB.QueryRow(ctx, `SELECT photo.begin_capture_draft($1,$2,$3,$4,$5,$6,$7)`,
		l.TenantID, l.JobID, l.Token, run, workflow, string(body), reservedBytes).Scan(&begun)
	if err != nil {
		return CaptureDraftConfig{}, err
	}
	if !begun {
		return CaptureDraftConfig{}, ErrInvalid
	}
	return config, nil
}

func (s SQLCaptureAssessmentStore) LoadDraft(ctx context.Context, l CaptureAssessmentLease) (CaptureDraftConfig, error) {
	if s.DB == nil || !validCaptureLease(l) {
		return CaptureDraftConfig{}, ErrInvalid
	}
	var raw []byte
	var expectedSHA string
	err := s.DB.QueryRow(ctx, `SELECT config,config_sha FROM photo.capture_draft_config($1,$2,$3)`,
		l.TenantID, l.JobID, l.Token).Scan(&raw, &expectedSHA)
	if err != nil {
		return CaptureDraftConfig{}, err
	}
	if len(raw) < 2 || len(raw) > 2<<20 {
		return CaptureDraftConfig{}, ErrInvalid
	}
	var config CaptureDraftConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF {
		return CaptureDraftConfig{}, ErrInvalid
	}
	sha, err := canonical.Hash(config)
	paramsSHA, paramsErr := canonical.Hash(config.Capture.Params)
	paramsJCS, bodyErr := canonical.Marshal(config.Capture.Params)
	if err != nil || paramsErr != nil || bodyErr != nil || sha != expectedSHA || paramsSHA != config.Capture.ParamsSHA ||
		string(paramsJCS) != config.Capture.ParamsJCS || config.ProjectID != l.ProjectID || config.Capture.JobID != l.JobID ||
		config.Capture.Params.Binding.AssetVersionID != l.AssetVersionID {
		return CaptureDraftConfig{}, ErrInvalid
	}
	frozenPolicy, err := canonical.Bytes(l.Policy)
	policy, policyErr := canonical.Bytes(config.Capture.Policy)
	if err != nil || policyErr != nil || !bytes.Equal(frozenPolicy, policy) {
		return CaptureDraftConfig{}, ErrInvalid
	}
	return config, nil
}
