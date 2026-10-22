package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"syscall"
)

var ErrAuthoredFinalUnavailable = errors.New("AUTHORED_FINAL_DEPLOYMENT_UNAVAILABLE")
var ErrAuthoredFinalInstalledCaptureRequired = errors.New("AUTHORED_FINAL_INSTALLED_CHAIN_CAPTURE_REQUIRED")

type AuthoredFinalTenantGeneration struct {
	Tenant   uuid.UUID   `json:"tenant_id"`
	Evidence v2input.Ref `json:"generation_evidence"`
}
type AuthoredFinalDeployment struct {
	Generations            []AuthoredFinalTenantGeneration `json:"generations,omitempty"`
	SchemaVersion          int                             `json:"schema_version"`
	Tenants                []uuid.UUID                     `json:"tenants"`
	CalibrationPath        string                          `json:"calibration_path"`
	CalibrationSHA         string                          `json:"calibration_sha256"`
	NativeReplayPolicyPath string                          `json:"native_replay_policy_path,omitempty"`
	NativeReplayPolicySHA  string                          `json:"native_replay_policy_sha256,omitempty"`
}

func (d AuthoredFinalDeployment) validate() error {
	if (d.SchemaVersion != 1 && d.SchemaVersion != 2 && d.SchemaVersion != 3) || len(d.Tenants) < 1 || len(d.Tenants) > 256 || (d.SchemaVersion != 3 && (!filepath.IsAbs(d.CalibrationPath) || !deploymentDigest(d.CalibrationSHA))) {
		return ErrAuthoredFinalUnavailable
	}
	if d.SchemaVersion == 1 {
		if d.NativeReplayPolicyPath != "" || d.NativeReplayPolicySHA != "" {
			return ErrAuthoredFinalUnavailable
		}
	} else if !filepath.IsAbs(d.NativeReplayPolicyPath) || !deploymentDigest(d.NativeReplayPolicySHA) {
		return ErrAuthoredFinalUnavailable
	}
	if d.SchemaVersion == 3 {
		if d.CalibrationPath != "" || d.CalibrationSHA != "" || len(d.Generations) != len(d.Tenants) {
			return ErrAuthoredFinalUnavailable
		}
		bindings := map[uuid.UUID]bool{}
		for _, g := range d.Generations {
			if g.Tenant == uuid.Nil || bindings[g.Tenant] || g.Evidence.ArtifactID == uuid.Nil || !deploymentDigest(g.Evidence.SHA256) {
				return ErrAuthoredFinalUnavailable
			}
			bindings[g.Tenant] = true
		}
		for _, tenant := range d.Tenants {
			if !bindings[tenant] {
				return ErrAuthoredFinalUnavailable
			}
		}
	} else if len(d.Generations) != 0 {
		return ErrAuthoredFinalUnavailable
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range d.Tenants {
		if id == uuid.Nil || seen[id] {
			return ErrAuthoredFinalUnavailable
		}
		seen[id] = true
	}
	return nil
}
func LoadAuthoredFinalDeployment(path, pin string) (AuthoredFinalDeployment, error) {
	var zero AuthoredFinalDeployment
	if !filepath.IsAbs(path) || !deploymentDigest(pin) {
		return zero, ErrAuthoredFinalUnavailable
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return zero, ErrAuthoredFinalUnavailable
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 2 || before.Size() > 1048576 {
		return zero, ErrAuthoredFinalUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1048577))
	if err != nil || int64(len(raw)) != before.Size() {
		return zero, ErrAuthoredFinalUnavailable
	}
	after, err := f.Stat()
	if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return zero, ErrAuthoredFinalUnavailable
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != pin {
		return zero, ErrAuthoredFinalUnavailable
	}
	var d AuthoredFinalDeployment
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || decoder.Decode(new(any)) != io.EOF || d.validate() != nil {
		return zero, ErrAuthoredFinalUnavailable
	}
	return d, nil
}

func buildAuthoredFinalParentScheduler(ctx context.Context, scheduler *pgxpool.Pool, objects *objectstore.Store, bucket string) (*AuthoredFinalParentProcessor, func(), error) {
	path, pin, dsn := os.Getenv("PHOTO_AUTHORED_FINAL_DEPLOYMENT"), os.Getenv("PHOTO_AUTHORED_FINAL_DEPLOYMENT_SHA256"), os.Getenv("PHOTO_AUTHORED_FINAL_API_DATABASE_URL")
	if path == "" && pin == "" && dsn == "" {
		return nil, func() {}, nil
	}
	d, err := LoadAuthoredFinalDeployment(path, pin)
	if err != nil {
		return nil, nil, err
	}
	if scheduler == nil || objects == nil || bucket == "" || dsn == "" {
		return nil, nil, ErrAuthoredFinalUnavailable
	}
	var catalog *v2render.PinnedAuthoredFinalCalibrationCatalog
	if d.SchemaVersion != 3 {
		catalog, err = v2render.LoadPinnedAuthoredFinalCalibrationCatalog(d.CalibrationPath, d.CalibrationSHA)
		if err != nil {
			return nil, nil, err
		}
	}
	// This function has NO fallback implementation in this source packet. It must
	// be emitted from a genuine installed-head capture and attest every actual
	// input/execution/adoption/public-seal hook before configured startup succeeds.
	var present bool
	err = scheduler.QueryRow(ctx, `SELECT coalesce((SELECT p.prosecdef AND p.proowner='photo_fn_owner'::regrole AND has_function_privilege(current_user,p.oid,'EXECUTE') FROM pg_proc p WHERE p.oid=to_regprocedure('photo.authored_render_final_dispatch_installed()')),false)`).Scan(&present)
	if err != nil {
		return nil, nil, err
	}
	if !present {
		return nil, nil, ErrAuthoredFinalInstalledCaptureRequired
	}
	var installed bool
	if err = scheduler.QueryRow(ctx, `SELECT photo.authored_render_final_dispatch_installed() IS TRUE`).Scan(&installed); err != nil {
		return nil, nil, err
	}
	if !installed {
		return nil, nil, ErrAuthoredFinalInstalledCaptureRequired
	}
	if d.SchemaVersion == 3 {
		// This attestation must be generated from the real installed generation
		// catalog/profile/input/seal/adoption/cleanup chain. There is no fallback.
		var ready bool
		if err = scheduler.QueryRow(ctx, `SELECT coalesce((SELECT p.prosecdef AND p.proowner='photo_fn_owner'::regrole AND has_function_privilege(current_user,p.oid,'EXECUTE') FROM pg_proc p WHERE p.oid=to_regprocedure('photo.authored_render_final_root3_dispatch_installed()')),false)`).Scan(&ready); err != nil {
			return nil, nil, err
		}
		if !ready {
			return nil, nil, ErrAuthoredFinalInstalledCaptureRequired
		}
		if err = scheduler.QueryRow(ctx, `SELECT photo.authored_render_final_root3_dispatch_installed() IS TRUE`).Scan(&ready); err != nil {
			return nil, nil, err
		}
		if !ready {
			return nil, nil, ErrAuthoredFinalInstalledCaptureRequired
		}
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, nil, ErrAuthoredFinalUnavailable
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	database.GuardApplicationRole(cfg)
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, ErrAuthoredFinalUnavailable
	}
	closeAPI := func() { api.Close() }
	if err = database.RequireApplicationRole(ctx, api, "photo_api"); err != nil {
		closeAPI()
		return nil, nil, err
	}
	var preparation *v2render.SQLAuthoredFinalContinuation
	if d.SchemaVersion == 3 {
		generations := map[uuid.UUID]v2input.Ref{}
		for _, g := range d.Generations {
			generations[g.Tenant] = g.Evidence
		}
		preparation, err = v2render.NewSQLAuthoredFinalRoot3TenantContinuation(scheduler, api, objects, bucket, generations)
	} else {
		preparation, err = v2render.NewSQLAuthoredFinalContinuation(scheduler, api, objects, bucket, catalog)
	}
	if err != nil {
		closeAPI()
		return nil, nil, err
	}
	if d.SchemaVersion >= 2 {
		native := v2render.SQLAuthoredNativeReplayService{Scheduler: scheduler, Objects: objects, Bucket: bucket}
		replay, err := v2render.NewSQLAuthoredFinalReplayPreparation(native, d.NativeReplayPolicyPath, d.NativeReplayPolicySHA)
		if err != nil {
			closeAPI()
			return nil, nil, err
		}
		preparation.Preparation.NativeReplays = replay
	}
	return &AuthoredFinalParentProcessor{DB: scheduler, Preparation: preparation, tenants: append([]uuid.UUID(nil), d.Tenants...), after: map[uuid.UUID]uuid.UUID{}}, closeAPI, nil
}
