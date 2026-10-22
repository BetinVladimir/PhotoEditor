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
	"photoeditor/backend/internal/v2render"
	"syscall"
)

var ErrAuthoredDetailUnavailable = errors.New("AUTHORED_DETAIL_DEPLOYMENT_UNAVAILABLE")
var ErrAuthoredDetailInstalledCaptureRequired = errors.New("AUTHORED_DETAIL_INSTALLED_CHAIN_CAPTURE_REQUIRED")

type AuthoredDetailDeployment struct {
	SchemaVersion   int         `json:"schema_version"`
	Tenants         []uuid.UUID `json:"tenants"`
	CalibrationPath string      `json:"calibration_path"`
	CalibrationSHA  string      `json:"calibration_sha256"`
	MaskRAM         int64       `json:"mask_ram_bytes"`
	MaskScratch     int64       `json:"mask_scratch_bytes"`
	MaskOutput      int64       `json:"mask_output_bytes"`
}

func (d AuthoredDetailDeployment) validate() error {
	if d.SchemaVersion != 1 || len(d.Tenants) < 1 || len(d.Tenants) > 256 || !filepath.IsAbs(d.CalibrationPath) || !deploymentDigest(d.CalibrationSHA) || d.MaskRAM < 1073741824 || d.MaskScratch < 1 || d.MaskOutput < 1 || d.MaskRAM > 9007199254740991 || d.MaskScratch > 9007199254740991 || d.MaskOutput > 9007199254740991 {
		return ErrAuthoredDetailUnavailable
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range d.Tenants {
		if id == uuid.Nil || seen[id] {
			return ErrAuthoredDetailUnavailable
		}
		seen[id] = true
	}
	return nil
}
func LoadAuthoredDetailDeployment(path, pin string) (AuthoredDetailDeployment, error) {
	var zero AuthoredDetailDeployment
	if !filepath.IsAbs(path) || !deploymentDigest(pin) {
		return zero, ErrAuthoredDetailUnavailable
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return zero, ErrAuthoredDetailUnavailable
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 2 || before.Size() > 1048576 {
		return zero, ErrAuthoredDetailUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1048577))
	if err != nil || int64(len(raw)) != before.Size() {
		return zero, ErrAuthoredDetailUnavailable
	}
	after, err := f.Stat()
	if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return zero, ErrAuthoredDetailUnavailable
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != pin {
		return zero, ErrAuthoredDetailUnavailable
	}
	var d AuthoredDetailDeployment
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || decoder.Decode(new(any)) != io.EOF || d.validate() != nil {
		return zero, ErrAuthoredDetailUnavailable
	}
	return d, nil
}

func buildAuthoredDetailParentScheduler(ctx context.Context, scheduler *pgxpool.Pool, objects *objectstore.Store, bucket string) (*AuthoredDetailParentProcessor, func(), error) {
	path, pin, dsn := os.Getenv("PHOTO_AUTHORED_DETAIL_DEPLOYMENT"), os.Getenv("PHOTO_AUTHORED_DETAIL_DEPLOYMENT_SHA256"), os.Getenv("PHOTO_AUTHORED_DETAIL_API_DATABASE_URL")
	if path == "" && pin == "" && dsn == "" {
		return nil, func() {}, nil
	}
	d, err := LoadAuthoredDetailDeployment(path, pin)
	if err != nil {
		return nil, nil, err
	}
	if scheduler == nil || objects == nil || bucket == "" || dsn == "" {
		return nil, nil, ErrAuthoredDetailUnavailable
	}
	catalog, err := v2render.LoadPinnedAuthoredSegmentCalibrationCatalog(d.CalibrationPath, d.CalibrationSHA)
	if err != nil {
		return nil, nil, err
	}
	// This function has NO fallback implementation in this source packet. It must
	// be emitted from a genuine installed-head capture and attest every actual
	// input/execution/adoption/public-seal hook before configured startup succeeds.
	var present bool
	err = scheduler.QueryRow(ctx, `SELECT coalesce((SELECT p.prosecdef AND p.proowner='photo_fn_owner'::regrole AND has_function_privilege(current_user,p.oid,'EXECUTE') FROM pg_proc p WHERE p.oid=to_regprocedure('photo.authored_render_detail_dispatch_installed()')),false)`).Scan(&present)
	if err != nil {
		return nil, nil, err
	}
	if !present {
		return nil, nil, ErrAuthoredDetailInstalledCaptureRequired
	}
	var installed bool
	if err = scheduler.QueryRow(ctx, `SELECT photo.authored_render_detail_dispatch_installed() IS TRUE`).Scan(&installed); err != nil {
		return nil, nil, err
	}
	if !installed {
		return nil, nil, ErrAuthoredDetailInstalledCaptureRequired
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, nil, ErrAuthoredDetailUnavailable
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	database.GuardApplicationRole(cfg)
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, ErrAuthoredDetailUnavailable
	}
	closeAPI := func() { api.Close() }
	if err = database.RequireApplicationRole(ctx, api, "photo_api"); err != nil {
		closeAPI()
		return nil, nil, err
	}
	preparation, err := v2render.NewSQLAuthoredDetailMaskPreparation(scheduler, api, objects, bucket, catalog, d.MaskRAM, d.MaskScratch, d.MaskOutput)
	if err != nil {
		closeAPI()
		return nil, nil, err
	}
	return &AuthoredDetailParentProcessor{DB: scheduler, Preparation: preparation, tenants: append([]uuid.UUID(nil), d.Tenants...), after: map[uuid.UUID]uuid.UUID{}}, closeAPI, nil
}
