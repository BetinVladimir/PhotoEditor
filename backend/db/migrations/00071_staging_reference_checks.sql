-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.guard_run_snapshot_artifacts() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE aid uuid;a photo.artifacts%ROWTYPE;
BEGIN
 IF TG_OP='UPDATE' AND NEW.tenant_id=OLD.tenant_id AND NEW.input_snapshot_id=OLD.input_snapshot_id
 AND NOT(OLD.status IN ('completed','completed_with_errors','failed','cancelled') AND NEW.status IN ('draft','queued','running','waiting_review','cancelling')) THEN RETURN NEW;END IF;
 FOR aid IN
  SELECT artifact_id FROM photo.snapshot_items WHERE tenant_id=NEW.tenant_id AND snapshot_id=NEW.input_snapshot_id AND artifact_id IS NOT NULL
  UNION SELECT artifact_id FROM photo.snapshots WHERE tenant_id=NEW.tenant_id AND id=NEW.input_snapshot_id AND artifact_id IS NOT NULL
  ORDER BY artifact_id
 LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=NEW.tenant_id AND id=aid FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'SNAPSHOT_ARTIFACT_MISSING' USING ERRCODE='23503';END IF;
  IF a.state<>'ready' OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN
   RAISE EXCEPTION 'SNAPSHOT_ARTIFACT_NOT_READY' USING ERRCODE='23514';END IF;
 END LOOP;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_run_snapshot_artifacts() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_run_snapshot_artifacts() FROM PUBLIC;
CREATE TRIGGER ready_snapshot_artifacts BEFORE INSERT OR UPDATE ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_run_snapshot_artifacts();

-- Internal predicate: the caller must hold the artifact's conflicting row lock
-- and prove the owner ingest/upload settled. It never grants deletion by itself.
-- +goose StatementBegin
CREATE FUNCTION photo.staging_has_references(p_tenant uuid,p_artifact uuid,p_run uuid,p_upload uuid,p_snapshot uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE relation record;extra text;found_ref boolean;
BEGIN
 IF p_tenant IS NULL OR p_artifact IS NULL OR p_run IS NULL OR p_upload IS NULL OR p_snapshot IS NULL THEN RETURN true;END IF;
 -- Reusing the owner's snapshot in any other run retains its source.
 IF EXISTS(SELECT FROM photo.runs WHERE tenant_id=p_tenant AND input_snapshot_id=p_snapshot AND id<>p_run) THEN RETURN true;END IF;
 FOR relation IN
  SELECT DISTINCT rel.relname AS table_name,local_col.attname AS column_name
  FROM pg_constraint c JOIN pg_class rel ON rel.oid=c.conrelid JOIN pg_namespace ns ON ns.oid=rel.relnamespace
  CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY local_key(attnum,ord)
  JOIN pg_attribute local_col ON local_col.attrelid=rel.oid AND local_col.attnum=local_key.attnum
  JOIN pg_attribute remote_col ON remote_col.attrelid=c.confrelid AND remote_col.attnum=c.confkey[local_key.ord::integer]
  WHERE c.contype='f' AND c.confrelid='photo.artifacts'::regclass AND ns.nspname='photo' AND remote_col.attname='id'
 LOOP
  extra:='';
  IF relation.table_name='upload_sessions' AND relation.column_name='staging_artifact_id' THEN extra:=' AND id<>$4';
  ELSIF relation.table_name='snapshot_items' AND relation.column_name='artifact_id' THEN extra:=' AND snapshot_id<>$5';
  ELSIF relation.table_name='ingest_node_plans' AND relation.column_name='source_artifact_id' THEN extra:=' AND run_id<>$3';
  ELSIF relation.table_name IN ('ingest_originals','ingest_upload_bindings') AND relation.column_name='source_id' THEN extra:=' AND run_id<>$3';
  ELSIF relation.table_name='artifact_leases' AND relation.column_name='artifact_id' THEN extra:=' AND expires_at>clock_timestamp()';
  END IF;
  EXECUTE format('SELECT EXISTS(SELECT FROM photo.%I WHERE tenant_id=$1 AND %I=$2%s)',relation.table_name,relation.column_name,extra)
  INTO found_ref USING p_tenant,p_artifact,p_run,p_upload,p_snapshot;
  IF found_ref THEN RETURN true;END IF;
 END LOOP;
 -- Only terminal owner-run command inputs are historical. Foreign task refs and
 -- any result output retain the artifact, including cache/replay consumers.
 IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_artifact::text))
  AND (run_id<>p_run OR state NOT IN ('succeeded','failed','cancelled','skipped')))
 OR EXISTS(SELECT FROM photo.task_results WHERE tenant_id=p_tenant AND outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_artifact::text))) THEN RETURN true;END IF;
 RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.staging_has_references(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.staging_has_references(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
