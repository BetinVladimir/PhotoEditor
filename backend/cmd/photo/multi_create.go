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
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2optical"
	"time"
)

func buildMulti(ctx context.Context, objects *objectstore.Store, bucket, operation string) (httpapi.MultiSubmit, func(), error) {
	noop := func() {}
	stem := ""
	switch operation {
	case "optical.panorama":
		stem = "PANORAMA"
	case "optical.focus_stack":
		stem = "FOCUS_STACK"
	default:
		return nil, noop, errors.New("unsupported multi-image operation")
	}
	path := os.Getenv("PHOTO_V29_" + stem + "_CONFIG")
	if path == "" {
		return nil, noop, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dsn := os.Getenv("V29_" + stem + "_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		return nil, noop, errors.New("multi-image scheduler configuration unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, noop, errors.New("multi-image configuration unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil, noop, errors.New("multi-image configuration invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil {
		return nil, noop, errors.New("multi-image configuration read failed")
	}
	expectedSHA := os.Getenv("PHOTO_V29_" + stem + "_CONFIG_SHA256")
	if _, err := domain.ParseDigest(expectedSHA); err != nil {
		return nil, noop, errors.New("multi-image configuration digest unavailable")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return nil, noop, errors.New("multi-image configuration digest mismatch")
	}
	deployment, err := v2optical.DecodeMultiDeployment(operation, raw)
	if err != nil {
		return nil, noop, errors.New("multi-image configuration invalid")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("multi-image scheduler configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	database.GuardApplicationRole(config)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, noop, errors.New("multi-image scheduler unavailable")
	}
	var role string
	var installed bool
	err = pool.QueryRow(ctx, `SELECT current_user,
 (SELECT count(*)=17 AND bool_and(p.prosecdef AND owner.rolname='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE'))
 FROM unnest(ARRAY[
 'photo.seal_v29_multi_graph(uuid,uuid,uuid,text,jsonb)',
 'photo.read_v29_multi_queued_receipt(uuid,uuid,uuid,uuid,jsonb)',
 'photo.v29_multi_array_source(uuid,uuid,uuid,uuid,uuid,text,text,integer,text,uuid,text,text)',
 'photo.register_v29_multi_transport(uuid,uuid,text,jsonb,jsonb,text)',
 'photo.v29_multi_source_document(uuid,uuid,uuid,uuid,uuid,jsonb)',
 'photo.claim_v29_multi_intent(uuid,uuid,uuid,uuid,text,text,text)',
 'photo.lock_v29_multi_qualification_objects(uuid,uuid,uuid,uuid,jsonb,jsonb,text)',
 'photo.save_v29_multi_draft(uuid,uuid,uuid,text)',
 'photo.reserve_v29_multi_params_document(uuid,uuid,uuid,text,text,bigint,text,jsonb)',
 'photo.complete_v29_multi_params_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb)',
 'photo.stage_v29_multi_task(uuid,uuid,text,jsonb)',
 'photo.reserve_v29_multi_transport_document(uuid,uuid,uuid,text,text,bigint,text,jsonb,text)',
 'photo.complete_v29_multi_transport_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb,text)',
 'photo.read_v29_multi_published_transport(uuid,uuid,uuid,text,jsonb)',
 'photo.v29_multi_qualification_sources(uuid,uuid,uuid,uuid,jsonb,text)',
 'photo.lock_v29_multi_qualification(uuid,uuid,uuid,uuid,jsonb,text)',
 'photo.v29_multi_capabilities_ready(uuid,uuid,uuid,uuid,jsonb,text)'
 ]) wanted(signature) JOIN pg_proc p ON p.oid=to_regprocedure(wanted.signature) JOIN pg_roles owner ON owner.oid=p.proowner)
 AND to_regprocedure('photo.seal_v29_multi_graph_pre_claim(uuid,uuid,uuid,text,jsonb)') IS NOT NULL
 AND NOT has_function_privilege(current_user,'photo.seal_v29_multi_graph_pre_claim(uuid,uuid,uuid,text,jsonb)','EXECUTE')
 AND to_regprocedure('photo.read_v29_multi_queued_receipt_pre_claim(uuid,uuid,uuid,uuid,jsonb)') IS NOT NULL
 AND NOT has_function_privilege(current_user,'photo.read_v29_multi_queued_receipt_pre_claim(uuid,uuid,uuid,uuid,jsonb)','EXECUTE')`).Scan(&role, &installed)
	if err != nil || role != "photo_scheduler" || !installed {
		pool.Close()
		return nil, noop, errors.New("multi-image installation unavailable")
	}
	service, err := deployment.Build(pool, objects, bucket)
	if err != nil {
		pool.Close()
		return nil, noop, errors.New("multi-image deployment refused")
	}
	return service, pool.Close, nil
}
