package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type semanticSceneStageSaved struct {
	Config    json.RawMessage `json:"config_snapshot"`
	SHA       string          `json:"config_hash"`
	RunState  string          `json:"run_state"`
	TaskState string          `json:"task_state"`
	Sealed    bool            `json:"sealed"`
}
type semanticSceneSavedMarker struct {
	Schema      int                            `json:"schema_version"`
	Operation   string                         `json:"operation"`
	Parent      uuid.UUID                      `json:"parent_run_id"`
	RequestSHA  string                         `json:"request_sha"`
	Session     uuid.UUID                      `json:"auth_session_id"`
	Source      v2input.SessionVersionBinding  `json:"source"`
	BindingSHA  string                         `json:"source_binding_sha"`
	Document    uuid.UUID                      `json:"params_document"`
	Params      v2input.Ref                    `json:"params"`
	TaskID      uuid.UUID                      `json:"task_id"`
	TaskSHA     string                         `json:"task_sha"`
	InputDigest string                         `json:"input_digest"`
	Task        v2input.DraftTask              `json:"task"`
	Reserved    int64                          `json:"reserved_bytes"`
	Expected    v2qualification.Expected       `json:"qualification_expected"`
	Report      personEvidencePin              `json:"qualification_report"`
	Costs       v2input.SemanticQualifiedCosts `json:"qualified_costs"`
}

func (s SQLTransferSourceReader) readSemanticSceneStageSaved(ctx context.Context, who auth.Identity, request Request, anchor semanticSceneStageAnchor) (semanticSceneStageSaved, bool, error) {
	var zero semanticSceneStageSaved
	if err := verifySemanticSceneStageAnchor(ctx, who, s.ProjectID, request, anchor); err != nil {
		return zero, false, err
	}
	if s.DB == nil {
		return zero, false, v2input.ErrInvalid
	}
	var raw []byte
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT photo.semantic_scene_saved_config($1,$2,$3,$4,$5,$6,$7)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID, request.RequestSHA, anchor.run).Scan(&raw)
	})
	if err != nil {
		return zero, false, err
	}
	if len(raw) == 0 {
		return zero, false, ctx.Err()
	}
	var saved semanticSceneStageSaved
	if len(raw) > 5<<20 || json.Unmarshal(raw, &saved) != nil || len(saved.Config) < 2 || len(saved.Config) > 4<<20 {
		return zero, false, v2input.ErrInvalid
	}
	sha, err := canonical.Hash(saved.Config)
	if err != nil || sha != saved.SHA {
		return zero, false, v2input.ErrConflict
	}
	return saved, true, ctx.Err()
}

// Frozen measured costs, pins, resource limits and reservation come from the
// admitted child's marker, never from current deployment configuration. Physical
// evidence is reread with those exact pins; withdrawn qualification refuses.
func (s SQLTransferSourceReader) restoreSemanticSceneStage(ctx context.Context, who auth.Identity, request Request, anchor semanticSceneStageAnchor, saved semanticSceneStageSaved, color TransferColorProfile, publisher v2draft.Publisher, catalog v2draft.ArtifactCatalog) (semanticSceneStagePlan, error) {
	var zero semanticSceneStagePlan
	var config struct {
		Project uuid.UUID                `json:"project_id"`
		Marker  semanticSceneSavedMarker `json:"v27_scene_prepare"`
	}
	if json.Unmarshal(saved.Config, &config) != nil {
		return zero, v2input.ErrInvalid
	}
	m := config.Marker
	if config.Project != s.ProjectID || m.Schema != 1 || m.Operation != anchor.operation || m.Parent != request.RunID || m.RequestSHA != request.RequestSHA || m.Session != who.SessionID || m.Source != anchor.source || m.BindingSHA != anchor.bindingSHA || m.Document != anchor.document || m.TaskID != anchor.task || m.Task.ID != anchor.task || m.Params.ArtifactID != m.Task.ParamsArtifactID || m.Params.SHA256 != m.Task.ParamsSHA || m.Task.InputDigest != m.InputDigest || m.Expected.Operation != anchor.operation || m.Report.Contract != "QualificationReport" || m.Report.Bucket != s.Bucket || m.Report.Backend != m.Expected.Backend || (m.Expected.Backend != "cuda" && m.Expected.Backend != "mps") {
		return zero, v2input.ErrConflict
	}
	taskSHA, err := canonical.Hash(m.Task)
	if err != nil || taskSHA != m.TaskSHA {
		return zero, v2input.ErrConflict
	}
	var profile struct {
		Backend  string `json:"backend"`
		Estimate struct {
			RAM     int64 `json:"ram_bytes"`
			VRAM    int64 `json:"vram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
			Output  int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(m.Task.ExecutionProfile, &profile) != nil || profile.Backend != m.Expected.Backend {
		return zero, v2input.ErrInvalid
	}
	producer := TransferSceneProducer{Expected: ownQualificationExpected(m.Expected), Report: v2input.Ref{ArtifactID: m.Report.ArtifactID, SHA256: m.Report.SHA256}, Costs: m.Costs}
	params, err := s.publishSemanticSceneStageParams(ctx, who, request, anchor, color, publisher, producer)
	if err != nil {
		return zero, err
	}
	if params.ref != m.Params {
		return zero, v2input.ErrConflict
	}
	budget := nativePreparationBudget{Costs: m.Costs, RAM: profile.Estimate.RAM, VRAM: profile.Estimate.VRAM, Scratch: profile.Estimate.Scratch, Output: profile.Estimate.Output, Reserved: m.Reserved}
	prepared, err := s.prepareSemanticSceneStageTask(ctx, who, request, params, catalog, producer, budget)
	if err != nil {
		return zero, err
	}
	plan, err := s.compileSemanticSceneStagePlan(ctx, who, request, prepared, m.Reserved)
	if err != nil {
		return zero, err
	}
	savedCanonical, err := canonical.Marshal(saved.Config)
	if err != nil || string(plan.shell.Config) != string(savedCanonical) || plan.shell.ConfigSHA != saved.SHA {
		return zero, v2input.ErrConflict
	}
	return plan, ctx.Err()
}
