package main

import (
 "photoeditor/backend/internal/database"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticreview"
)

func readPinnedExposureCatalog(path, expected string) (semanticreview.ExposurePreparationCatalog, error) {
	empty := semanticreview.ExposurePreparationCatalog{}
	digest, err := hex.DecodeString(expected)
	if path == "" || len(digest) != 32 || err != nil || hex.EncodeToString(digest) != expected {
		return empty, errors.New("semantic exposure configuration invalid")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&022 != 0 || before.Size() < 2 || before.Size() > 4<<20 {
		return empty, errors.New("semantic exposure configuration unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return empty, errors.New("semantic exposure configuration unavailable")
	}
	opened, statErr := file.Stat()
	if statErr != nil || !os.SameFile(before, opened) {
		file.Close()
		return empty, errors.New("semantic exposure configuration changed")
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 4<<20+1))
	after, afterErr := file.Stat()
	closeErr := file.Close()
	if readErr != nil || afterErr != nil || closeErr != nil || len(raw) > 4<<20 || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return empty, errors.New("semantic exposure configuration changed")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != expected {
		return empty, errors.New("semantic exposure configuration digest mismatch")
	}
	catalog, err := semanticreview.DecodeExposurePreparationCatalog(raw)
	if err != nil {
		return empty, errors.New("semantic exposure configuration invalid")
	}
	return catalog, nil
}

func buildSemanticExposureApply(ctx context.Context, api *pgxpool.Pool, objects *objectstore.Store, bucket string, previews ...*semanticreview.ExposurePreviewProvider) (httpapi.SemanticReviewApply, func(), error) {
	noop := func() {}
	if len(previews) > 1 {
		return nil, noop, errors.New("ambiguous semantic preview provider")
	}
	var preview *semanticreview.ExposurePreviewProvider
	if len(previews) == 1 {
		preview = previews[0]
	}
	return buildSemanticShownApply(ctx, api, objects, bucket, preview, nil, nil)
}

func buildSemanticShownApply(ctx context.Context, api *pgxpool.Pool, objects *objectstore.Store, bucket string,
	preview *semanticreview.ExposurePreviewProvider, removal *semanticreview.RemovalPreviewProvider, cleanup *semanticreview.BackgroundCleanupPreviewProvider, retouchProviders ...*semanticreview.RetouchPreviewProvider) (httpapi.SemanticReviewApply, func(), error) {
	noop := func() {}
	if len(retouchProviders) > 1 {
		return nil, noop, errors.New("ambiguous retouch preview provider")
	}
	var retouch *semanticreview.RetouchPreviewProvider
	if len(retouchProviders) == 1 {
		retouch = retouchProviders[0]
	}
	path, sha, dsn := os.Getenv("PHOTO_SEMANTIC_EXPOSURE_CONFIG_FILE"), os.Getenv("PHOTO_SEMANTIC_EXPOSURE_CONFIG_SHA256"), os.Getenv("PHOTO_SEMANTIC_ADMISSION_DATABASE_URL")
	if path == "" && sha == "" && dsn == "" {
		return nil, noop, nil
	}
	if path == "" || sha == "" || dsn == "" || api == nil || objects == nil {
		return nil, noop, errors.New("semantic exposure dependencies unavailable")
	}
	if os.Getenv("PHOTO_SEMANTIC_REVIEW_CONFIG_FILE") != "" || os.Getenv("PHOTO_SEMANTIC_REVIEW_CONFIG_SHA256") != "" {
		return nil, noop, errors.New("semantic review configurations conflict")
	}
	catalog, err := readPinnedExposureCatalog(path, sha)
	if err != nil {
		return nil, noop, err
	}
	iccRoot := os.Getenv("PHOTO_ICC_ROOT")
	if iccRoot == "" {
		return nil, noop, errors.New("semantic exposure ICC unavailable")
	}
	iccFile, err := os.Open(filepath.Join(iccRoot, "work_rec2020_gamma24_v1.icc"))
	if err != nil {
		return nil, noop, errors.New("semantic exposure ICC unavailable")
	}
	icc, readErr := io.ReadAll(io.LimitReader(iccFile, 4097))
	closeErr := iccFile.Close()
	if readErr != nil || closeErr != nil || len(icc) > 4096 {
		return nil, noop, errors.New("semantic exposure ICC unavailable")
	}
	color, err := semanticreview.NewExposureColorProfile(icc)
	if err != nil {
		return nil, noop, errors.New("semantic exposure ICC invalid")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("semantic admission database configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_semantic_admission"
	database.GuardApplicationRole(config)
	config.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	config.MaxConns = 8
	config.MinConns = 0
	admission, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, noop, errors.New("semantic admission database unavailable")
	}
	var safeLogin bool
	err = admission.QueryRow(ctx, `SELECT session_user<>current_user
 AND EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND rolcanlogin AND NOT(rolsuper OR rolinherit OR rolbypassrls OR rolcreaterole OR rolcreatedb OR rolreplication))
 AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=session_user::regrole AND roleid<>'photo_semantic_admission'::regrole)
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='photo' AND c.relkind IN('r','p') AND
 (c.relowner=session_user::regrole OR has_table_privilege(session_user,c.oid,'DELETE,TRUNCATE,TRIGGER,REFERENCES') OR has_any_column_privilege(session_user,c.oid,'INSERT') OR has_any_column_privilege(session_user,c.oid,'UPDATE')))`).Scan(&safeLogin)
	if err != nil || !safeLogin {
		admission.Close()
		return nil, noop, errors.New("semantic admission login unsafe or unavailable")
	}
	service, err := semanticreview.InstallTenantExposureApplyService(ctx, semanticreview.ExposureApplyInstallation{API: api, Admission: admission, Objects: objects, Bucket: bucket, Color: color, ShownPreview: preview, ShownRemovalPreview: removal, ShownBackgroundCleanupPreview: cleanup, ShownRetouchPreview: retouch}, catalog)
	if err != nil {
		admission.Close()
		return nil, noop, errors.New("semantic exposure installation unavailable")
	}
	return service, admission.Close, nil
}
