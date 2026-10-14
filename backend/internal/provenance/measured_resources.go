package provenance

import (
	"context"
	"time"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

// Installed deployment evidence, never request data. Entries qualify exact
// workloads; no interpolation across image sizes, SDKs or hardware is allowed.
// The report hash must be approved by the deployment qualification process.
type SigningResourceMeasurement struct {
	WorkloadSHA string `json:"workload_sha256"`
	RAM         int64  `json:"ram_bytes"`
	Scratch     int64  `json:"scratch_bytes"`
	Output      int64  `json:"output_bytes"`
	InputJSON   int64  `json:"max_input_json_bytes"`
}
type SigningResourceMeasurements struct {
	Schema       string                       `json:"schema_version"`
	Runtime      string                       `json:"runtime_sha256"`
	SDK          string                       `json:"sdk_lock_sha256"`
	Contracts    string                       `json:"contracts_sha256"`
	Hardware     string                       `json:"hardware_sha256"`
	NotBefore    string                       `json:"not_before"`
	NotAfter     string                       `json:"not_after"`
	Measurements []SigningResourceMeasurement `json:"measurements"`
}
type SigningResourceInstallation struct{ ReportSHA, RuntimeSHA, SDKLockSHA, ContractsSHA, HardwareSHA string }
type MeasuredSigningResourceQualifier struct {
	registry     *SigningPolicyRegistry
	clock        func() time.Time
	sdk          string
	from, until  time.Time
	measurements map[string]SigningTaskResources
}

func NewMeasuredSigningResourceQualifier(raw []byte, installed SigningResourceInstallation, registry *SigningPolicyRegistry, clock func() time.Time) (*MeasuredSigningResourceQualifier, error) {
	if registry == nil || clock == nil || len(raw) < 2 || len(raw) > 8<<20 || !digest(installed.ReportSHA) || bodyDigest(raw) != installed.ReportSHA || !digest(installed.RuntimeSHA) || !digest(installed.SDKLockSHA) || !digest(installed.ContractsSHA) || !digest(installed.HardwareSHA) {
		return nil, ErrInvalid
	}
	var report SigningResourceMeasurements
	if decode(raw, &report, 8<<20) != nil || report.Schema != "signing-resource-measurements/v1" || report.Runtime != installed.RuntimeSHA || report.SDK != installed.SDKLockSHA || report.Contracts != installed.ContractsSHA || report.Hardware != installed.HardwareSHA || len(report.Measurements) < 1 || len(report.Measurements) > 4096 {
		return nil, ErrInvalid
	}
	from, err := time.Parse(time.RFC3339Nano, report.NotBefore)
	if err != nil || from.IsZero() {
		return nil, ErrInvalid
	}
	until, err := time.Parse(time.RFC3339Nano, report.NotAfter)
	if err != nil || !until.After(from) {
		return nil, ErrInvalid
	}
	q := &MeasuredSigningResourceQualifier{registry: registry, clock: clock, sdk: installed.SDKLockSHA, from: from, until: until, measurements: map[string]SigningTaskResources{}}
	const safe int64 = 9007199254740991
	for _, m := range report.Measurements {
		if !digest(m.WorkloadSHA) || m.InputJSON < 1<<20 || m.InputJSON > 128<<20 || m.RAM < m.InputJSON || m.RAM > safe || m.Scratch < 1<<30 || m.Scratch > safe || m.Output < (512<<20)+(16<<20) || m.Output > safe {
			return nil, ErrInvalid
		}
		if _, duplicate := q.measurements[m.WorkloadSHA]; duplicate {
			return nil, ErrInvalid
		}
		q.measurements[m.WorkloadSHA] = SigningTaskResources{ContractsSHA: installed.ContractsSHA, RAMBytes: m.RAM, ScratchBytes: m.Scratch, OutputBytes: m.Output, MaxInputJSONBytes: m.InputJSON}
	}
	return q, nil
}

// Params SHA covers exact export/lineage/policy refs and policy ID. Lineage body
// is separately validated so a supplied hash cannot conceal replacement bytes.
func SigningResourceWorkloadSHA(intent FrozenSigningIntent, evidence SigningExportEvidence) (string, error) {
	if !signingEvidenceMatches(intent, evidence) {
		return "", ErrInvalid
	}
	params, err := DecodeSignParams(intent.params)
	if err != nil {
		return "", err
	}
	if _, err := DecodeSigningLineage(intent.lineage, params.Lineage); err != nil {
		return "", err
	}
	if evidence.MIME != "image/jpeg" && evidence.MIME != "image/png" {
		return "", ErrInvalid
	}
	return canonical.Hash(struct {
		Params string `json:"params_sha256"`
		MIME   string `json:"mime"`
	}{Params: bodyDigest(intent.params), MIME: evidence.MIME})
}
func (q *MeasuredSigningResourceQualifier) QualifySigningResources(ctx context.Context, who auth.Identity, intent FrozenSigningIntent, evidence SigningExportEvidence) (SigningTaskResources, error) {
	var zero SigningTaskResources
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if q == nil || q.registry == nil || q.clock == nil || who.UserID != intent.actor || who.SessionID != intent.session {
		return zero, ErrInvalid
	}
	now := q.clock()
	if now.IsZero() || now.Before(q.from) || !now.Before(q.until) {
		return zero, ErrInvalid
	}
	workload, err := SigningResourceWorkloadSHA(intent, evidence)
	if err != nil {
		return zero, err
	}
	resources, exists := q.measurements[workload]
	if !exists {
		return zero, ErrInvalid
	}
	params, err := DecodeSignParams(intent.params)
	if err != nil {
		return zero, err
	}
	registry, err := q.registry.ForTenant(intent.tenant)
	if err != nil {
		return zero, err
	}
	policy, err := registry.Resolve(ctx, params, evidence.MIME, now)
	if err != nil {
		return zero, err
	}
	if policy.SDKLockSHA != q.sdk {
		return zero, ErrInvalid
	}
	// Recheck all frozen identity/policy/lineage invariants and protocol floors.
	if _, err := CompileSigningTask(ctx, intent, registry, evidence.MIME, resources, now); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	finished := q.clock()
	if finished.IsZero() || finished.Before(q.from) || !finished.Before(q.until) {
		return zero, ErrInvalid
	}
	return resources, nil
}

var _ SigningResourceQualifier = (*MeasuredSigningResourceQualifier)(nil)

// QualifyDelegatedSigningResources uses the installed measured workload and
// live operator policy for a Delivery child. The parent scheduler must supply
// an independently resolved ready export and repeat its authority at admission.
func (q *MeasuredSigningResourceQualifier) QualifyDelegatedSigningResources(ctx context.Context, actor uuid.UUID, intent FrozenSigningIntent, ready ReadySigningExport) (SigningTaskResources, error) {
	var zero SigningTaskResources
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if q == nil || q.registry == nil || q.clock == nil || actor == uuid.Nil ||
		!intent.delegated || intent.session != uuid.Nil || intent.actor != actor ||
		ready.Tenant != intent.tenant || ready.Project != intent.project ||
		ready.ExportID != intent.export || ready.Version == uuid.Nil ||
		ready.Task == uuid.Nil || ready.Run == uuid.Nil || !ref(ready.Ref) ||
		ready.Object.SHA256 != ready.Ref.SHA256 || ready.Object.Key == "" ||
		ready.Object.VersionID == "" || ready.Object.VersionID == "null" ||
		ready.Object.Bytes < 1 || ready.Object.Bytes > 512<<20 {
		return zero, ErrInvalid
	}
	params, err := DecodeSignParams(intent.params)
	if err != nil || params.Export != ready.Ref {
		return zero, ErrInvalid
	}
	evidence := SigningExportEvidence{
		Project: ready.Project, ExportID: ready.ExportID, Export: ready.Ref,
		MIME: ready.Object.ContentType, Lineage: params.Lineage,
		LineageBody: append([]byte(nil), intent.lineage...),
	}
	now := q.clock()
	if now.IsZero() || now.Before(q.from) || !now.Before(q.until) {
		return zero, ErrInvalid
	}
	workload, err := SigningResourceWorkloadSHA(intent, evidence)
	if err != nil {
		return zero, err
	}
	resources, exists := q.measurements[workload]
	if !exists {
		return zero, ErrInvalid
	}
	registry, err := q.registry.ForTenant(intent.tenant)
	if err != nil {
		return zero, err
	}
	policy, err := registry.Resolve(ctx, params, evidence.MIME, now)
	if err != nil {
		return zero, err
	}
	if policy.SDKLockSHA != q.sdk {
		return zero, ErrInvalid
	}
	if _, err := CompileDelegatedSigningTask(ctx, intent, registry, evidence.MIME, resources, now); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	finished := q.clock()
	if finished.IsZero() || finished.Before(q.from) || !finished.Before(q.until) {
		return zero, ErrInvalid
	}
	return resources, nil
}
