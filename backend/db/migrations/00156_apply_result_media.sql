-- Accepted apply masters enter the same durable derivative pipeline as ingest
-- masters.  Origin remains explicit so every retry is revalidated against the
-- immutable source run and version before object I/O.
-- +goose Up
ALTER TABLE photo.media_retry_jobs DROP CONSTRAINT media_job_origin_ck;
ALTER TABLE photo.media_retry_jobs ADD CONSTRAINT media_job_origin_ck CHECK(
 (origin='bulk_repair' AND bulk_operation_id IS NOT NULL AND item_id IS NOT NULL AND origin_run_id IS NULL)
 OR (origin IN ('ingest','apply') AND bulk_operation_id IS NULL AND item_id IS NULL AND origin_run_id IS NOT NULL));
DROP INDEX photo.media_ingest_job_once;
CREATE UNIQUE INDEX media_origin_job_once ON photo.media_retry_jobs(tenant_id,origin,origin_run_id,asset_version_id) WHERE origin IN ('ingest','apply');

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.enqueue_ingest_media() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE kind_ref text;
BEGIN
 IF NEW.created_by_run_id IS NULL OR NEW.review_state<>'accepted' OR (TG_OP='UPDATE' AND OLD.review_state='accepted') THEN RETURN NEW;END IF;
 SELECT kind INTO kind_ref FROM photo.runs WHERE tenant_id=NEW.tenant_id AND id=NEW.created_by_run_id;
 IF kind_ref IN ('ingest','apply') THEN
  INSERT INTO photo.media_retry_jobs(event_id,tenant_id,asset_version_id,variants,origin,origin_run_id)
  VALUES(gen_random_uuid(),NEW.tenant_id,NEW.id,'["thumbnail","preview"]',kind_ref,NEW.created_by_run_id)
  ON CONFLICT(tenant_id,origin,origin_run_id,asset_version_id) WHERE origin IN ('ingest','apply') DO NOTHING;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER enqueue_accepted_media AFTER UPDATE OF review_state ON photo.asset_versions FOR EACH ROW EXECUTE FUNCTION photo.enqueue_ingest_media();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_media_retry_job()
RETURNS TABLE(event_id uuid,materialize_token uuid,tenant_id uuid,bulk_id uuid,item_id uuid,asset_version_id uuid,
 variants jsonb,master_artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,
 media_type text,width integer,height integer,output_role text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.media_retry_jobs%ROWTYPE;token uuid:=gen_random_uuid();a photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO j FROM photo.media_retry_jobs x WHERE x.status='queued' OR (x.status='materializing' AND x.materialize_lease_until<=clock_timestamp())
 ORDER BY x.created_at,x.event_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 IF (j.origin='bulk_repair' AND (NOT EXISTS(SELECT FROM photo.bulk_operations b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
   WHERE b.tenant_id=j.tenant_id AND b.id=j.bulk_operation_id AND b.action='retry_media' AND NOT b.cancel_requested
   AND b.status IN('completed','completed_with_errors') AND NOT r.cancel_requested)
   OR NOT EXISTS(SELECT FROM photo.collection_items i WHERE i.tenant_id=j.tenant_id AND i.id=j.item_id AND i.asset_version_id=j.asset_version_id)))
 OR (j.origin IN ('ingest','apply') AND NOT EXISTS(SELECT FROM photo.runs r JOIN photo.asset_versions v ON v.tenant_id=r.tenant_id AND v.created_by_run_id=r.id
   JOIN photo.assets x ON x.tenant_id=v.tenant_id AND x.id=v.asset_id WHERE r.tenant_id=j.tenant_id AND r.id=j.origin_run_id
   AND r.kind=j.origin AND NOT r.cancel_requested AND v.id=j.asset_version_id AND v.review_state='accepted' AND x.upload_state='verified' AND x.deleted_at IS NULL)) THEN
  UPDATE photo.media_retry_jobs AS target SET status='cancelled',error_code='SOURCE_CHANGED',materialize_token=NULL,materialize_lease_until=NULL WHERE target.event_id=j.event_id;RETURN;
 END IF;
 SELECT ar.* INTO a FROM photo.asset_versions v JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id
 WHERE v.tenant_id=j.tenant_id AND v.id=j.asset_version_id AND v.review_state='accepted' AND ar.state='ready'
 AND ar.object_version IS NOT NULL AND ar.object_version NOT IN('','null') AND ar.bytes>0 AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp());
 IF NOT FOUND OR a.output_role IS DISTINCT FROM 'working_image' OR a.media_type<>'application/json' THEN
  UPDATE photo.media_retry_jobs AS target SET status='failed',error_code='MASTER_UNAVAILABLE',materialize_token=NULL,materialize_lease_until=NULL WHERE target.event_id=j.event_id;RETURN;END IF;
 UPDATE photo.media_retry_jobs AS target SET status='materializing',materialize_token=token,materialize_lease_until=clock_timestamp()+interval '60 seconds',error_code=NULL WHERE target.event_id=j.event_id;
 RETURN QUERY SELECT j.event_id,token,j.tenant_id,coalesce(j.bulk_operation_id,'00000000-0000-0000-0000-000000000000'::uuid),coalesce(j.item_id,'00000000-0000-0000-0000-000000000000'::uuid),j.asset_version_id,j.variants,
  a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,a.width,a.height,a.output_role;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.begin_media_retry_plan(p_event uuid,p_token uuid,p_run uuid,p_hash text,p_config jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.media_retry_jobs%ROWTYPE;b photo.bulk_operations%ROWTYPE;source photo.runs%ROWTYPE;r photo.runs%ROWTYPE;snapshot_ref uuid;creator uuid;
BEGIN
 IF p_hash !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_config)<>'object' OR p_config<>jsonb_build_object('event_id',p_event,'asset_version_id',p_config->'asset_version_id','variants',p_config->'variants','srgb_sha256',p_config->'srgb_sha256') THEN RETURN false;END IF;
 SELECT * INTO j FROM photo.media_retry_jobs WHERE event_id=p_event FOR UPDATE;
 IF NOT FOUND OR j.status<>'materializing' OR j.materialize_token<>p_token OR j.materialize_lease_until<=clock_timestamp() OR j.asset_version_id<>(p_config->>'asset_version_id')::uuid OR j.variants<>p_config->'variants' THEN RETURN false;END IF;
 IF j.origin='bulk_repair' THEN SELECT * INTO b FROM photo.bulk_operations WHERE tenant_id=j.tenant_id AND id=j.bulk_operation_id;IF NOT FOUND OR b.cancel_requested THEN RETURN false;END IF;snapshot_ref:=b.snapshot_id;creator:=b.created_by;
 ELSE SELECT * INTO source FROM photo.runs WHERE tenant_id=j.tenant_id AND id=j.origin_run_id;IF NOT FOUND OR source.kind<>j.origin OR source.cancel_requested THEN RETURN false;END IF;snapshot_ref:=source.input_snapshot_id;creator:=source.created_by;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=j.tenant_id AND id=p_run;
 IF FOUND THEN RETURN r.kind='preview' AND r.input_snapshot_id=snapshot_ref AND r.config_hash=p_hash AND r.config_snapshot=p_config AND r.created_by=creator AND NOT r.graph_sealed;END IF;
 IF EXISTS(SELECT FROM photo.runs WHERE id=p_run) THEN RETURN false;END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(p_run,j.tenant_id,'preview','queued',snapshot_ref,p_config,p_hash,creator);
 UPDATE photo.media_retry_jobs SET run_id=p_run WHERE event_id=p_event;RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
