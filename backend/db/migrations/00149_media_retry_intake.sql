-- +goose Up
CREATE TABLE photo.media_retry_jobs (
 event_id uuid PRIMARY KEY,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 tenant_id uuid NOT NULL,
 bulk_operation_id uuid NOT NULL,
 item_id uuid NOT NULL,
 asset_version_id uuid NOT NULL,
 variants jsonb NOT NULL CHECK(jsonb_typeof(variants)='array' AND jsonb_array_length(variants) BETWEEN 1 AND 2),
 status text NOT NULL DEFAULT 'queued' CHECK(status IN('queued','materializing','running','completed','failed','cancelled')),
 materialize_token uuid,
 materialize_lease_until timestamptz,
 run_id uuid,
 error_code text,
 UNIQUE(tenant_id,event_id),
 UNIQUE(tenant_id,bulk_operation_id,item_id)
);
ALTER TABLE photo.media_retry_jobs ADD FOREIGN KEY(tenant_id) REFERENCES photo.tenants(id);
ALTER TABLE photo.media_retry_jobs ADD FOREIGN KEY(tenant_id,bulk_operation_id) REFERENCES photo.bulk_operations(tenant_id,id);
ALTER TABLE photo.media_retry_jobs ADD FOREIGN KEY(tenant_id,item_id) REFERENCES photo.collection_items(tenant_id,id);
ALTER TABLE photo.media_retry_jobs ADD FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id);
ALTER TABLE photo.media_retry_jobs ADD FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.media_retry_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.media_retry_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.media_retry_jobs TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.media_retry_jobs TO photo_fn_owner;

-- The collection worker only persists an intent after proving that the command
-- is the exact outbox event emitted for an already frozen bulk snapshot item.
-- +goose StatementBegin
CREATE FUNCTION photo.accept_media_retry_command(p_event uuid,p_bulk uuid,p_item uuid,p_version uuid,p_variants jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.bulk_operations%ROWTYPE;tenant_ref uuid;ordinal_ref bigint;old photo.media_retry_jobs%ROWTYPE;
BEGIN
 IF p_event IS NULL OR p_bulk IS NULL OR p_item IS NULL OR p_version IS NULL OR jsonb_typeof(p_variants)<>'array'
 OR jsonb_array_length(p_variants) NOT BETWEEN 1 AND 2
 OR EXISTS(SELECT FROM jsonb_array_elements_text(p_variants) v(x) WHERE x NOT IN('thumbnail','preview'))
 OR (SELECT count(*)<>count(DISTINCT x) FROM jsonb_array_elements_text(p_variants) v(x)) THEN
  RAISE EXCEPTION 'INVALID_MEDIA_RETRY_COMMAND' USING ERRCODE='22023';END IF;
 SELECT * INTO b FROM photo.bulk_operations WHERE id=p_bulk FOR SHARE;
 IF NOT FOUND OR b.action<>'retry_media' OR b.params<>jsonb_build_object('variants',p_variants) THEN RETURN false;END IF;
 tenant_ref:=b.tenant_id;
 SELECT si.ordinal INTO ordinal_ref FROM photo.snapshot_items si WHERE si.tenant_id=b.tenant_id AND si.snapshot_id=b.snapshot_id
  AND si.item_id=p_item AND si.asset_version_id=p_version;
 IF ordinal_ref IS NULL OR NOT EXISTS(SELECT FROM photo.bulk_item_results r WHERE r.tenant_id=b.tenant_id
  AND r.bulk_operation_id=b.id AND r.snapshot_ordinal=ordinal_ref AND r.status='succeeded') THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.outbox o WHERE o.tenant_id=b.tenant_id AND o.event_id=p_event
  AND o.destination='photo.commands.v1' AND o.routing_key='media.retry'
  AND o.payload=jsonb_build_object('schema_version',1,'event_id',p_event,'bulk_id',p_bulk,'item_id',p_item,'asset_version_id',p_version,'variants',p_variants)) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.media_retry_jobs WHERE event_id=p_event FOR UPDATE;
 IF FOUND THEN RETURN old.tenant_id=b.tenant_id AND old.bulk_operation_id=p_bulk AND old.item_id=p_item
  AND old.asset_version_id=p_version AND old.variants=p_variants;END IF;
 IF EXISTS(SELECT FROM photo.media_retry_jobs j WHERE j.tenant_id=b.tenant_id AND j.bulk_operation_id=p_bulk AND j.item_id=p_item) THEN RETURN false;END IF;
 INSERT INTO photo.media_retry_jobs(event_id,tenant_id,bulk_operation_id,item_id,asset_version_id,variants)
 VALUES(p_event,b.tenant_id,p_bulk,p_item,p_version,p_variants);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.accept_media_retry_command(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.accept_media_retry_command(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.accept_media_retry_command(uuid,uuid,uuid,uuid,jsonb) TO photo_worker;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
