-- +goose Up
-- Genuine offset-qualified capture times first; floating local times are unknown.
-- +goose StatementBegin
CREATE FUNCTION photo.collection_capture_instant(p_value text) RETURNS timestamptz
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog AS $$
BEGIN
 IF p_value IS NULL OR p_value !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?(Z|[+-][0-9]{2}:[0-9]{2})$' THEN RETURN NULL;END IF;
 RETURN p_value::timestamptz;
EXCEPTION WHEN invalid_datetime_format OR datetime_field_overflow THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.collection_capture_instant(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.collection_capture_instant(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.collection_capture_instant(text) TO photo_api,photo_fn_owner;

-- SECURITY INVOKER preserves API tenant RLS; portal uses it only from its
-- existing SECURITY DEFINER authorized list. No object locator is exposed.
-- +goose StatementBegin
CREATE FUNCTION photo.collection_item_order(p_tenant uuid,p_collection uuid)
RETURNS TABLE(item_id uuid,sort_ordinal bigint)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,photo AS $$
 SELECT i.id,row_number() OVER(ORDER BY
  CASE WHEN c.sort_mode='captured_at' THEN coalesce(
   CASE WHEN a.metadata->'ingest'->'source'->>'sha256'=original.sha256 THEN photo.collection_capture_instant(a.metadata->'ingest'->>'captured_at') END,
   capture.instant) END ASC NULLS LAST,
  CASE WHEN c.sort_mode='filename' THEN lower(nullif(u.filename,'')) END COLLATE "C" ASC NULLS LAST,
  i.position,i.id)
 FROM photo.collections c JOIN photo.collection_items i ON i.tenant_id=c.tenant_id AND i.collection_id=c.id
 JOIN photo.assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id
 JOIN photo.artifacts original ON original.tenant_id=a.tenant_id AND original.id=a.original_artifact_id
 LEFT JOIN photo.ingest_upload_bindings b ON b.tenant_id=a.tenant_id AND b.asset_id=a.id AND b.project_id=c.project_id AND b.source_sha=original.sha256
 LEFT JOIN photo.upload_sessions u ON u.tenant_id=b.tenant_id AND u.id=b.upload_id AND u.ingest_run_id=b.run_id AND u.project_id=b.project_id
 LEFT JOIN LATERAL(SELECT CASE WHEN count(DISTINCT r.captured_at)=1 THEN min(r.captured_at) END AS instant
  FROM photo.capture_receipts r WHERE r.tenant_id=b.tenant_id AND r.project_id=b.project_id AND r.upload_id=b.upload_id AND r.content_sha=b.source_sha) capture ON true
 WHERE c.tenant_id=p_tenant AND c.id=p_collection AND c.deleted_at IS NULL
 AND (current_user='photo_fn_owner' OR p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.collection_item_order(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.collection_item_order(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.collection_item_order(uuid,uuid) TO photo_api,photo_fn_owner;

-- Preserve the installed portal function identity, return shape and ACL chain.
-- Its internal bigint cursor position becomes an ordinal under current revision.
-- +goose StatementBegin
DO $$
DECLARE definition text;anchor constant text:=' FROM photo.collection_items i';
BEGIN
 definition:=pg_get_functiondef('photo.list_portal_items(bytea,uuid,bigint,uuid,integer)'::regprocedure);
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1
 OR (length(definition)-length(replace(definition,'i.position','')))/length('i.position')<>3 THEN RAISE EXCEPTION 'COLLECTION_PORTAL_SORT_SHAPE';END IF;
 definition:=replace(definition,anchor,anchor||' JOIN photo.collection_item_order(c.tenant_id,c.id) ordering ON ordering.item_id=i.id');
 definition:=replace(definition,'i.position','ordering.sort_ordinal');
 EXECUTE definition;
END $$;
-- +goose StatementEnd

-- Extend only verified inspect metadata sideband, never pixel/image contracts.
-- +goose StatementBegin
DO $$
DECLARE definition text;anchor constant text:='p_metadata-''source''-''width''-''height''-''orientation''-''has_alpha''-''format''-''sensor_alignment''';
BEGIN
 definition:=pg_get_functiondef('photo.adopt_ingest_image(uuid,uuid,bigint,uuid,text,jsonb,jsonb)'::regprocedure);
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'COLLECTION_CAPTURE_METADATA_SHAPE';END IF;
 definition:=replace(definition,anchor,anchor||'-''captured_at''-''captured_local''');
 definition:=replace(definition,' -- Immutable hint only;',E' IF p_metadata ? ''captured_at'' AND p_metadata ? ''captured_local'' THEN RETURN false;END IF;\n IF p_metadata ? ''captured_at'' AND (jsonb_typeof(p_metadata->''captured_at'') IS DISTINCT FROM ''string'' OR photo.collection_capture_instant(p_metadata->>''captured_at'') IS NULL) THEN RETURN false;END IF;\n IF p_metadata ? ''captured_local'' AND (jsonb_typeof(p_metadata->''captured_local'') IS DISTINCT FROM ''string'' OR p_metadata->>''captured_local'' !~ ''^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}$'') THEN RETURN false;END IF;\n IF p_metadata ? ''captured_local'' THEN BEGIN PERFORM (p_metadata->>''captured_local'')::timestamp; EXCEPTION WHEN invalid_datetime_format OR datetime_field_overflow THEN RETURN false;END;END IF;\n -- Immutable hint only;');
 IF strpos(definition,'p_metadata ? ''captured_local''')=0 THEN RAISE EXCEPTION 'COLLECTION_CAPTURE_METADATA_GUARD_SHAPE';END IF;
 EXECUTE definition;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
