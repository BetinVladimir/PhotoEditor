package workflow

import (
	"context"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type CaptureObjects interface {
	artifactjson.Objects
	v2input.SessionAnalysisObjectReader
}
type CaptureSchedulerConfig struct {
	Backend                                          string
	ModelLockSHA                                     string
	RAMLimit, VRAMLimit, ScratchLimit, ReservedBytes int64
}

// NewCaptureProcessor wires both durable phases to the same storage publisher
// and admission transaction. Operation qualification remains a SQL seal gate.
func NewCaptureProcessor(db *pgxpool.Pool, objects CaptureObjects, bucket string, cfg CaptureSchedulerConfig) (v2input.CaptureAssessmentOrchestrator, error) {
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 || (cfg.Backend != "cpu" && cfg.Backend != "cuda") || cfg.RAMLimit <= 0 || cfg.VRAMLimit < 0 || cfg.ScratchLimit <= 0 || cfg.ReservedBytes <= 0 || cfg.ReservedBytes > 9007199254740991 {
		return v2input.CaptureAssessmentOrchestrator{}, errors.New("invalid capture scheduler configuration")
	}
	if _, err := domain.ParseDigest(cfg.ModelLockSHA); err != nil {
		return v2input.CaptureAssessmentOrchestrator{}, errors.New("invalid capture model lock")
	}
	if _, err := v2input.CompileCapturePrepareProfile(cfg.Backend, 1, 1, false, cfg.RAMLimit, cfg.VRAMLimit, cfg.ScratchLimit); err != nil {
		return v2input.CaptureAssessmentOrchestrator{}, errors.New("capture resource budget cannot admit a frame")
	}
	store := v2input.SQLCaptureAssessmentStore{DB: db}
	source := v2input.SQLCaptureSource{DB: db, Objects: objects, Bucket: bucket}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: db, Bucket: bucket}, Objects: objects}
	admission := v2input.Admission{DB: db}
	contextBuilder := v2input.CaptureContextBuilder{Store: store, Shots: v2input.SQLCaptureShotSource{DB: db, Objects: objects, Bucket: bucket}}
	preparer := v2input.CaptureAssessmentPreparer{Store: store, Source: source,
		Prepared: v2input.SQLCapturePreparedSource{DB: db, Objects: objects, Bucket: bucket}, Context: contextBuilder, Documents: documents}
	preparation := v2input.CapturePreparationService{Store: store, Source: source, Documents: documents, Committer: admission,
		TransportForRun: func(lease v2input.CaptureAssessmentLease, run uuid.UUID) v2input.Publisher {
			return v2input.ScopedPublisher{Documents: documents, Tenant: lease.TenantID, Run: run}
		},
		Backend: cfg.Backend, ModelLockSHA: cfg.ModelLockSHA, RAMLimit: cfg.RAMLimit, VRAMLimit: cfg.VRAMLimit, ScratchLimit: cfg.ScratchLimit, ReservedBytes: cfg.ReservedBytes}
	assessment := v2input.PreparedCaptureAdmission{Source: preparer, Committer: admission,
		TransportForGraph: func(lease v2input.CaptureAssessmentLease, graph v2input.FrozenCaptureGraph) v2input.Publisher {
			return v2input.ScopedPublisher{Documents: documents, Tenant: lease.TenantID, Run: graph.Run}
		}}
	pipeline := v2input.CapturePipeline{Progress: store, Preparation: preparation, Assessment: assessment}
	return v2input.CaptureAssessmentOrchestrator{Store: store, Admission: captureClassifiedAdmission{pipeline}}, nil
}

type captureClassifiedAdmission struct {
	next v2input.CaptureAssessmentAdmission
}

func (a captureClassifiedAdmission) AdmitCapture(ctx context.Context, lease v2input.CaptureAssessmentLease) error {
	err := a.next.AdmitCapture(ctx, lease)
	if errors.Is(err, v2input.ErrInvalid) {
		return v2input.CapturePermanentFailure{Code: "CAPTURE_INPUT_INVALID"}
	}
	if errors.Is(err, v2input.ErrConflict) {
		return v2input.CapturePermanentFailure{Code: "CAPTURE_CONTEXT_CONFLICT"}
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		codes := map[string]string{"V24_IDENTITY_CONSENT_REVOKED": "CONSENT_REVOKED", "V24_CAPTURE_CONTEXT_STALE": "CAPTURE_CONTEXT_STALE", "V24_CAPTURE_HISTORY_UNAVAILABLE": "CAPTURE_HISTORY_UNAVAILABLE"}
		if code, ok := codes[pg.Message]; ok {
			return v2input.CapturePermanentFailure{Code: code}
		}
	}
	return err
}

// Capture configuration is explicit because the model lock and GPU budgets
// must match the qualified worker deployment.
func CaptureConfigFromEnvironment(getenv func(string) string) (CaptureSchedulerConfig, bool, error) {
	var cfg CaptureSchedulerConfig
	if getenv == nil {
		return cfg, false, errors.New("capture environment unavailable")
	}
	flag := getenv("PHOTO_CAPTURE_ENABLED")
	if flag == "" || flag == "false" {
		return cfg, false, nil
	}
	if flag != "true" {
		return cfg, false, errors.New("PHOTO_CAPTURE_ENABLED must be true or false")
	}
	cfg.Backend = getenv("PHOTO_CAPTURE_BACKEND")
	if cfg.Backend == "" {
		cfg.Backend = "cuda"
	}
	cfg.ModelLockSHA = getenv("PHOTO_CAPTURE_MODEL_LOCK_SHA")
	if cfg.Backend != "cpu" && cfg.Backend != "cuda" {
		return cfg, false, errors.New("invalid capture backend")
	}
	if _, err := domain.ParseDigest(cfg.ModelLockSHA); err != nil {
		return cfg, false, errors.New("capture model lock required")
	}
	fields := []struct {
		name     string
		target   *int64
		fallback int64
	}{
		{"PHOTO_CAPTURE_RAM_BYTES", &cfg.RAMLimit, 16 << 30},
		{"PHOTO_CAPTURE_VRAM_BYTES", &cfg.VRAMLimit, 10 << 30},
		{"PHOTO_CAPTURE_SCRATCH_BYTES", &cfg.ScratchLimit, 64 << 30},
		{"PHOTO_CAPTURE_RESERVED_BYTES", &cfg.ReservedBytes, 1 << 30},
	}
	for _, field := range fields {
		*field.target = field.fallback
		if value := getenv(field.name); value != "" {
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 || strconv.FormatInt(n, 10) != value {
				return cfg, false, errors.New("invalid capture resource budget")
			}
			*field.target = n
		}
	}
	if cfg.ReservedBytes < 1 || cfg.ReservedBytes > 9007199254740991 {
		return cfg, false, errors.New("invalid capture reservation")
	}
	if _, err := v2input.CompileCapturePrepareProfile(cfg.Backend, 1, 1, false, cfg.RAMLimit, cfg.VRAMLimit, cfg.ScratchLimit); err != nil {
		return cfg, false, errors.New("capture resource budget too small")
	}
	return cfg, true, nil
}

type captureStartupQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func captureSQLReady(ctx context.Context, db captureStartupQuery, backend string) (bool, error) {
	signatures := []string{
		"photo.capture_operations_ready(text)",
		"photo.begin_capture_draft(uuid,uuid,uuid,uuid,uuid,text,bigint)",
		"photo.bind_capture_preparation(uuid,uuid,uuid,uuid)",
		"photo.capture_assessment_source(uuid,uuid,uuid)",
		"photo.capture_draft_config(uuid,uuid,uuid)",
		"photo.capture_history_source(uuid,uuid,uuid)",
		"photo.capture_preparation_progress(uuid,uuid,uuid)",
		"photo.capture_prepared_artifact(uuid,uuid,uuid,text,text)",
		"photo.capture_prepared_source(uuid,uuid)",
		"photo.capture_shot_source(uuid,uuid,uuid)",
		"photo.capture_source_child(uuid,uuid,uuid,uuid,text,text)",
		"photo.claim_capture_assessment()",
		"photo.close_capture_assessment(uuid,uuid,uuid,uuid,text,text)",
		"photo.complete_v24_capture_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)",
		"photo.fail_capture_assessment(uuid,uuid,text)",
		"photo.freeze_capture_consents(uuid,uuid,uuid,uuid[])",
		"photo.freeze_capture_coverage(uuid,uuid,uuid)",
		"photo.freeze_capture_selection(uuid,uuid,uuid)",
		"photo.freeze_capture_snapshot(uuid,uuid,uuid,uuid,text)",
		"photo.lock_capture_assessment_commit(uuid,uuid,uuid,uuid,uuid)",
		"photo.release_capture_assessment(uuid,uuid,integer)",
		"photo.renew_capture_assessment(uuid,uuid)",
		"photo.reserve_v24_capture_document(uuid,uuid,uuid,text,text,bigint,text)",
		"photo.seal_capture_preparation(uuid,uuid,uuid,uuid,text)",
		"photo.stage_capture_assessment(uuid,uuid,uuid,uuid,uuid,text,text,jsonb)",
		"photo.stage_capture_document_body(uuid,uuid,uuid,text,text)",
		"photo.stage_capture_preparation(uuid,uuid,uuid,uuid,uuid,text,text,jsonb)",
	}
	var ready bool
	err := db.QueryRow(ctx, `SELECT NOT EXISTS(SELECT FROM unnest($1::text[]) required(signature)
 WHERE NOT coalesce(has_function_privilege(current_user,to_regprocedure(required.signature),'EXECUTE'),false))
  AND photo.capture_operations_ready($2)`, signatures, backend).Scan(&ready)
	return ready, err
}
