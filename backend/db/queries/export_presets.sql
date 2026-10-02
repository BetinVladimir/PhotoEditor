-- name: GetExportPreset :one
SELECT id,name,revision,config,config_hash,created_at
FROM photo.export_presets
WHERE tenant_id = sqlc.arg(tenant_id) AND id = sqlc.arg(id) AND archived_at IS NULL;

-- name: ListExportPresets :many
SELECT id,name,revision,config,config_hash,created_at
FROM photo.export_presets
WHERE tenant_id = sqlc.arg(tenant_id)
  AND archived_at IS NULL
  AND (sqlc.narg(after_time)::timestamptz IS NULL OR (created_at,id) > (sqlc.narg(after_time)::timestamptz,sqlc.narg(after_id)::uuid))
ORDER BY created_at,id
LIMIT sqlc.arg(row_limit);

-- name: CreateExportPreset :one
INSERT INTO photo.export_presets(tenant_id,name,config,config_hash)
VALUES(sqlc.arg(tenant_id),sqlc.arg(name),sqlc.arg(config),sqlc.arg(config_hash))
RETURNING id,name,revision,config,config_hash,created_at;

-- name: CountActiveExportPresets :one
SELECT count(*) FROM photo.export_presets
WHERE tenant_id = sqlc.arg(tenant_id) AND id = ANY(sqlc.arg(ids)::uuid[]) AND archived_at IS NULL;
