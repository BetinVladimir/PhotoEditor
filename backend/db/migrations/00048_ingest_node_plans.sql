-- +goose Up
CREATE TABLE photo.ingest_node_plans (
 id uuid PRIMARY KEY,tenant_id uuid NOT NULL,run_id uuid NOT NULL,ordinal bigint NOT NULL CHECK(ordinal BETWEEN 0 AND 9007199254740991),
 node_key text NOT NULL,operation text NOT NULL CHECK(operation IN ('asset.verify','asset.inspect','asset.decode')),
 source_artifact_id uuid NOT NULL,source_sha text NOT NULL CHECK(source_sha ~ '^[0-9a-f]{64}$'),
 params_sha text NOT NULL CHECK(params_sha ~ '^[0-9a-f]{64}$'),params jsonb NOT NULL CHECK(jsonb_typeof(params)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,id),UNIQUE(tenant_id,run_id,node_key),UNIQUE(tenant_id,run_id,ordinal,operation),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),
 FOREIGN KEY(tenant_id,source_artifact_id) REFERENCES photo.artifacts(tenant_id,id)
);
ALTER TABLE photo.ingest_node_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.ingest_node_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.ingest_node_plans TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.ingest_node_plans TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.register_ingest_node(p_tenant uuid,p_run uuid,p_id uuid,p_ordinal bigint,p_operation text,p_source uuid,p_source_sha text,p_params_sha text,p_params jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;old photo.ingest_node_plans%ROWTYPE;k text;
BEGIN
 IF p_id IS NULL OR p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 9007199254740991
 OR p_operation IS NULL OR p_operation NOT IN ('asset.verify','asset.inspect','asset.decode')
 OR p_source IS NULL OR p_source_sha IS NULL OR p_source_sha !~ '^[0-9a-f]{64}$'
 OR p_params_sha IS NULL OR p_params_sha !~ '^[0-9a-f]{64}$'
 OR p_params IS NULL OR jsonb_typeof(p_params) IS DISTINCT FROM 'object' OR octet_length(p_params::text)>1048576 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR r.status NOT IN ('queued','running') OR r.graph_sealed THEN RETURN false;END IF;
 k:=p_operation||'/'||lpad(p_ordinal::text,16,'0');
 IF p_params->'source' IS DISTINCT FROM jsonb_build_object('artifact_id',p_source::text,'sha256',p_source_sha)
 OR NOT EXISTS(SELECT FROM photo.snapshot_items s JOIN photo.artifacts a ON a.tenant_id=s.tenant_id AND a.id=s.artifact_id
  WHERE s.tenant_id=p_tenant AND s.snapshot_id=r.input_snapshot_id AND s.ordinal=p_ordinal AND s.artifact_id=p_source
  AND s.sha256=p_source_sha AND a.sha256=p_source_sha AND a.state='ready'
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND p_params->'expected_bytes'=to_jsonb(a.bytes)) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.ingest_node_plans WHERE tenant_id=p_tenant AND run_id=p_run AND ordinal=p_ordinal AND operation=p_operation;
 IF FOUND THEN RETURN old.id=p_id AND old.node_key=k AND old.source_artifact_id=p_source AND old.source_sha=p_source_sha
 AND old.params_sha=p_params_sha AND old.params=p_params;END IF;
 IF EXISTS(SELECT FROM photo.ingest_node_plans WHERE id=p_id) THEN RETURN false;END IF;
 INSERT INTO photo.ingest_node_plans(id,tenant_id,run_id,ordinal,node_key,operation,source_artifact_id,source_sha,params_sha,params)
 VALUES(p_id,p_tenant,p_run,p_ordinal,k,p_operation,p_source,p_source_sha,p_params_sha,p_params);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_ingest_node(uuid,uuid,uuid,bigint,text,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_ingest_node(uuid,uuid,uuid,bigint,text,uuid,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_ingest_node(uuid,uuid,uuid,bigint,text,uuid,text,text,jsonb) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
