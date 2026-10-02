-- +goose Up
-- All new relational/command references share-lock their artifact before use.
-- Cleanup must update ready→deleting under the conflicting artifact row lock.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_ready_artifact_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE current_row jsonb:=to_jsonb(NEW);previous_row jsonb;field text;ref jsonb;
 ids uuid[]:='{}';aid uuid;a photo.artifacts%ROWTYPE;tenant uuid:=(current_row->>'tenant_id')::uuid;check_all boolean;
BEGIN
 IF TG_OP='UPDATE' THEN previous_row:=to_jsonb(OLD);END IF;
 check_all:=TG_OP='INSERT' OR TG_TABLE_NAME='artifact_leases' OR current_row->'tenant_id' IS DISTINCT FROM previous_row->'tenant_id';
 FOREACH field IN ARRAY TG_ARGV LOOP
  IF NOT check_all AND current_row->field IS NOT DISTINCT FROM previous_row->field THEN CONTINUE;END IF;
  IF field IN ('inputs','outputs') THEN
   IF jsonb_typeof(current_row->field) IS DISTINCT FROM 'array' THEN RAISE EXCEPTION 'INVALID_ARTIFACT_REFERENCES' USING ERRCODE='23514';END IF;
   FOR ref IN SELECT value FROM jsonb_array_elements(current_row->field) LOOP
    IF ref->>'artifact_id' IS NULL THEN RAISE EXCEPTION 'INVALID_ARTIFACT_REFERENCE' USING ERRCODE='23514';END IF;
    ids:=array_append(ids,(ref->>'artifact_id')::uuid);
   END LOOP;
  ELSIF current_row->>field IS NOT NULL THEN ids:=array_append(ids,(current_row->>field)::uuid);
  END IF;
 END LOOP;
 FOR aid IN SELECT DISTINCT id FROM unnest(ids) id ORDER BY id LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=tenant AND id=aid FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'ARTIFACT_REFERENCE_MISSING' USING ERRCODE='23503';END IF;
  IF a.state<>'ready' OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN
   RAISE EXCEPTION 'ARTIFACT_REFERENCE_NOT_READY' USING ERRCODE='23514';END IF;
 END LOOP;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_ready_artifact_reference() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_ready_artifact_reference() FROM PUBLIC;

-- Discover every current tenant-scoped FK to artifact.id, including extensions
-- added after the generated baseline. New migrations adding artifact relations
-- must explicitly attach this same guard (or regenerate the trigger arguments).
-- +goose StatementBegin
DO $$
DECLARE table_row record;args text;columns text[];
BEGIN
 FOR table_row IN
  SELECT relation.relname AS table_name,array_agg(DISTINCT local_col.attname ORDER BY local_col.attname) AS cols
  FROM pg_constraint constraint_row
  JOIN pg_class relation ON relation.oid=constraint_row.conrelid
  JOIN pg_namespace ns ON ns.oid=relation.relnamespace
  CROSS JOIN LATERAL unnest(constraint_row.conkey) WITH ORDINALITY local_key(attnum,ord)
  JOIN pg_attribute local_col ON local_col.attrelid=relation.oid AND local_col.attnum=local_key.attnum
  JOIN pg_attribute remote_col ON remote_col.attrelid=constraint_row.confrelid AND remote_col.attnum=constraint_row.confkey[local_key.ord]
  WHERE constraint_row.contype='f' AND constraint_row.confrelid='photo.artifacts'::regclass AND ns.nspname='photo' AND remote_col.attname='id'
  GROUP BY relation.relname
 LOOP
  columns:=table_row.cols;
  IF table_row.table_name='tasks' THEN columns:=array_append(columns,'inputs');END IF;
  SELECT string_agg(quote_literal(value),',') INTO args FROM unnest(columns) value;
  EXECUTE format('CREATE TRIGGER ready_artifact_reference BEFORE INSERT OR UPDATE ON photo.%I FOR EACH ROW EXECUTE FUNCTION photo.guard_ready_artifact_reference(%s)',table_row.table_name,args);
 END LOOP;
 CREATE TRIGGER ready_artifact_reference BEFORE INSERT OR UPDATE ON photo.task_results FOR EACH ROW EXECUTE FUNCTION photo.guard_ready_artifact_reference('outputs');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
