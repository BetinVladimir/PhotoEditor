package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// CreateDeployment contains only owned, validated operator policy. It grants no
// tenant authority and cannot enable operation capabilities or qualification.
type CreateDeployment struct {
	projects []createProjectPolicy
	color    TransferColorProfile
}

type createExpectation struct {
	Operation        string    `json:"operation"`
	Version          string    `json:"version"`
	Backend          string    `json:"backend"`
	ModelLockSHA     string    `json:"model_lock_sha"`
	RuntimeLockSHA   string    `json:"runtime_lock_sha"`
	DeviceProfileSHA string    `json:"device_profile_sha"`
	DatasetSHA       string    `json:"dataset_sha"`
	IssuerID         uuid.UUID `json:"issuer_id"`
	RequiredChecks   []string  `json:"required_checks"`
	CalibrationSHAs  []string  `json:"calibration_shas"`
	TestFixtureSHAs  []string  `json:"test_fixture_shas"`
	EvidenceSHAs     []string  `json:"evidence_shas"`
	LicenseCleared   bool      `json:"license_cleared"`
}
type createPersonPolicy struct {
	Report   v2input.Ref       `json:"report"`
	Expected createExpectation `json:"expected"`
}
type createProjectPolicy struct {
	ProjectID     uuid.UUID                        `json:"project_id"`
	Qualification v2input.SemanticQualificationPin `json:"qualification"`
	Expected      createExpectation                `json:"expected"`
	Person        createPersonPolicy               `json:"person"`
	Costs         createResourceCosts              `json:"costs"`
	RAM           int64                            `json:"ram_bytes"`
	VRAM          int64                            `json:"vram_bytes"`
	Scratch       int64                            `json:"scratch_bytes"`
	Output        int64                            `json:"output_bytes"`
	Metadata      int64                            `json:"metadata_bytes"`
	Reserved      int64                            `json:"reserved_bytes"`
}

type createResourceCosts struct {
	RAM     int64 `json:"ram_bytes"`
	VRAM    int64 `json:"vram_bytes"`
	Scratch int64 `json:"scratch_bytes"`
}

func (c createExpectation) expected() (v2qualification.Expected, error) {
	var zero v2qualification.Expected
	if c.IssuerID == uuid.Nil || !c.LicenseCleared || len(c.RequiredChecks) < 1 || len(c.RequiredChecks) > 256 {
		return zero, v2input.ErrInvalid
	}
	checks := map[string]bool{}
	for _, check := range c.RequiredChecks {
		if check == "" || len(check) > 128 || checks[check] {
			return zero, v2input.ErrInvalid
		}
		checks[check] = true
	}
	sets := make([]map[string]struct{}, 3)
	for i, values := range [][]string{c.CalibrationSHAs, c.TestFixtureSHAs, c.EvidenceSHAs} {
		if len(values) < 1 || len(values) > 256 {
			return zero, v2input.ErrInvalid
		}
		sets[i] = map[string]struct{}{}
		for _, sha := range values {
			if _, err := domain.ParseDigest(sha); err != nil {
				return zero, v2input.ErrInvalid
			}
			if _, exists := sets[i][sha]; exists {
				return zero, v2input.ErrInvalid
			}
			sets[i][sha] = struct{}{}
		}
	}
	return v2qualification.Expected{Operation: c.Operation, Version: c.Version, Backend: c.Backend, ModelLockSHA: c.ModelLockSHA, RuntimeLockSHA: c.RuntimeLockSHA, DeviceProfileSHA: c.DeviceProfileSHA, DatasetSHA: c.DatasetSHA, IssuerID: c.IssuerID, RequiredChecks: append([]string(nil), c.RequiredChecks...), CalibrationSHAs: sets[0], TestFixtureSHAs: sets[1], EvidenceSHAs: sets[2], LicenseCleared: c.LicenseCleared}, nil
}
func (p createProjectPolicy) preparation(color TransferColorProfile) (TransferPreparation, error) {
	var zero TransferPreparation
	expected, err := p.Expected.expected()
	if err != nil {
		return zero, err
	}
	person, err := p.Person.Expected.expected()
	if err != nil {
		return zero, err
	}
	if p.ProjectID == uuid.Nil || p.Reserved <= 0 || p.Reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	c := TransferPreparation{Qualification: p.Qualification, Expected: expected, PersonProtection: &TransferPersonProtection{Report: p.Person.Report, Expected: person}, Color: color, Costs: v2input.SemanticQualifiedCosts{RAMBytes: p.Costs.RAM, VRAMBytes: p.Costs.VRAM, ScratchBytes: p.Costs.Scratch}, RAM: p.RAM, VRAM: p.VRAM, Scratch: p.Scratch, Output: p.Output, MetadataBytes: p.Metadata}
	if err := validateTransferPolicyConfiguration(c); err != nil {
		return zero, err
	}
	return c, nil
}

// DecodeCreateDeployment rejects duplicates, aliases/unknown fields, trailing
// JSON, unsafe integers, malformed pins and incomplete operator acceptance.
// ICC bytes are verified separately by LoadTransferColorProfile, never JSON.
func DecodeCreateDeployment(raw []byte, color TransferColorProfile) (CreateDeployment, error) {
	var zero CreateDeployment
	if len(raw) == 0 || len(raw) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var body struct {
		SchemaVersion int                   `json:"schema_version"`
		Projects      []createProjectPolicy `json:"projects"`
	}
	decoder := json.NewDecoder(bytes.NewReader(canonicalBody))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil {
		return zero, v2input.ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return zero, v2input.ErrInvalid
	}
	owned, err := canonical.Marshal(body)
	if err != nil || !bytes.Equal(owned, canonicalBody) {
		return zero, v2input.ErrInvalid
	}
	if body.SchemaVersion != 1 || len(body.Projects) < 1 || len(body.Projects) > 4096 {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, project := range body.Projects {
		if seen[project.ProjectID] {
			return zero, v2input.ErrConflict
		}
		seen[project.ProjectID] = true
		if _, err := project.preparation(color); err != nil {
			return zero, err
		}
	}
	return CreateDeployment{projects: body.Projects, color: color}, nil
}

// Build uses caller-owned pools/storage and repeats actual role validation. It
// cannot publish documents or change operation capabilities at installation.
func (d CreateDeployment) Build(ctx context.Context, api, scheduler *pgxpool.Pool, objects *objectstore.Store, bucket string) (*ProjectCreateService, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil || scheduler == nil || objects == nil {
		return nil, v2input.ErrInvalid
	}
	preparers := make([]TransferDraftPreparer, 0, len(d.projects))
	for _, project := range d.projects {
		policy, err := project.preparation(d.color)
		if err != nil {
			return nil, err
		}
		preparers = append(preparers, TransferDraftPreparer{Reader: SQLTransferSourceReader{DB: api, Objects: objects, Bucket: bucket, ProjectID: project.ProjectID}, Documents: v2draft.Publisher{DB: api, Objects: objects, Bucket: bucket}, Scheduler: scheduler, TransportDocuments: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: bucket}, Objects: objects}, Configuration: policy, ReservedBytes: project.Reserved})
	}
	return NewProjectCreateService(ctx, preparers)
}
