-- +goose Up
CREATE TABLE photo.style_candidates (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), created_at timestamptz NOT NULL DEFAULT now(), tenant_id uuid NOT NULL,
 style_id uuid NOT NULL, training_run_id uuid NOT NULL, config_artifact_id uuid NOT NULL, report_artifact_id uuid NOT NULL,
 config_hash text, config_json jsonb, metrics jsonb, low_confidence boolean NOT NULL DEFAULT true,
	accepted_version_id uuid,
 state text NOT NULL DEFAULT 'materializing', materialize_token uuid NOT NULL DEFAULT gen_random_uuid(), materialize_lease_until timestamptz NOT NULL DEFAULT clock_timestamp()+interval '60 seconds',
 error_code text, UNIQUE(tenant_id,id), UNIQUE(tenant_id,training_run_id),
 FOREIGN KEY(tenant_id) REFERENCES photo.tenants(id), FOREIGN KEY(tenant_id,style_id) REFERENCES photo.styles(tenant_id,id),
 FOREIGN KEY(tenant_id,training_run_id) REFERENCES photo.runs(tenant_id,id),
 FOREIGN KEY(tenant_id,config_artifact_id) REFERENCES photo.artifacts(tenant_id,id), FOREIGN KEY(tenant_id,report_artifact_id) REFERENCES photo.artifacts(tenant_id,id),
	FOREIGN KEY(tenant_id,accepted_version_id) REFERENCES photo.style_versions(tenant_id,id),
 CHECK(state IN ('materializing','ready','failed','accepted')), CHECK(config_hash IS NULL OR config_hash ~ '^[0-9a-f]{64}$'),
 CHECK((state IN ('ready','accepted'))=(config_hash IS NOT NULL AND config_json IS NOT NULL AND metrics IS NOT NULL)),
	CHECK((state='accepted')=(accepted_version_id IS NOT NULL)),
 CHECK(config_json IS NULL OR jsonb_typeof(config_json)='object'), CHECK(metrics IS NULL OR jsonb_typeof(metrics)='object'),
 CHECK(error_code IS NULL OR length(error_code) BETWEEN 1 AND 120)
);
ALTER TABLE photo.style_candidates ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.style_candidates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.style_candidates TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.style_candidates TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.style_candidates TO photo_api;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.style_candidates TO photo_fn_owner;

-- Claims only the exact artifacts attested by the canonical aggregate result.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_style_candidate()
RETURNS TABLE(candidate_id uuid,token uuid,tenant_id uuid,run_id uuid,style_id uuid,config_id uuid,config_bucket text,config_sha text,config_key text,config_version text,config_bytes bigint,config_media text,report_id uuid,report_bucket text,report_sha text,report_key text,report_version text,report_bytes bigint,report_media text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE picked record;c photo.style_candidates%ROWTYPE;
BEGIN
 SELECT r.tenant_id,r.id run_id,(r.config_snapshot->>'style_id')::uuid style_id,t.id task_id,z.outputs INTO picked
 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.operation='style.aggregate' AND t.operation_version='1' AND t.state='succeeded'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 LEFT JOIN photo.style_candidates existing ON existing.tenant_id=r.tenant_id AND existing.training_run_id=r.id
 WHERE r.kind='analyze' AND r.config_snapshot ? 'style_id' AND existing.id IS NULL AND NOT r.cancel_requested
 ORDER BY r.created_at,r.id LIMIT 1 FOR UPDATE OF r SKIP LOCKED;
 IF FOUND THEN
  INSERT INTO photo.style_candidates(tenant_id,style_id,training_run_id,config_artifact_id,report_artifact_id)
  SELECT picked.tenant_id,picked.style_id,picked.run_id,config.id,report.id
  FROM photo.artifacts config,photo.artifacts report
  WHERE config.tenant_id=picked.tenant_id AND config.created_by_task_id=picked.task_id AND config.output_role='style_candidate' AND config.state='ready'
   AND report.tenant_id=picked.tenant_id AND report.created_by_task_id=picked.task_id AND report.output_role='aggregate_report' AND report.state='ready'
   AND picked.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',config.id::text,'sha256',config.sha256))
   AND picked.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',report.id::text,'sha256',report.sha256))
  RETURNING * INTO c;
 END IF;
 IF c.id IS NULL THEN
  SELECT * INTO c FROM photo.style_candidates x WHERE x.state='materializing' AND x.materialize_lease_until<=clock_timestamp() ORDER BY x.created_at,x.id LIMIT 1 FOR UPDATE SKIP LOCKED;
  IF c.id IS NOT NULL THEN UPDATE photo.style_candidates SET materialize_token=gen_random_uuid(),materialize_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=c.id RETURNING * INTO c;END IF;
 END IF;
 IF c.id IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT c.id,c.materialize_token,c.tenant_id,c.training_run_id,c.style_id,ca.id,ca.bucket,ca.sha256,ca.object_key,ca.object_version,ca.bytes,ca.media_type,ra.id,ra.bucket,ra.sha256,ra.object_key,ra.object_version,ra.bytes,ra.media_type
 FROM photo.artifacts ca,photo.artifacts ra WHERE ca.tenant_id=c.tenant_id AND ca.id=c.config_artifact_id AND ca.state='ready' AND ca.object_version IS NOT NULL AND ca.object_version NOT IN ('','null')
 AND ra.tenant_id=c.tenant_id AND ra.id=c.report_artifact_id AND ra.state='ready' AND ra.object_version IS NOT NULL AND ra.object_version NOT IN ('','null');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_style_candidate() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_style_candidate() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_style_candidate() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.finish_style_candidate(p_candidate uuid,p_token uuid,p_config jsonb,p_hash text,p_metrics jsonb,p_low boolean,p_error text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c photo.style_candidates%ROWTYPE;
BEGIN
 SELECT * INTO c FROM photo.style_candidates WHERE id=p_candidate FOR UPDATE;
 IF NOT FOUND OR c.state<>'materializing' OR c.materialize_token<>p_token OR c.materialize_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF p_error IS NOT NULL THEN
  IF length(p_error) NOT BETWEEN 1 AND 120 THEN RAISE EXCEPTION 'invalid error' USING ERRCODE='22023';END IF;
  UPDATE photo.style_candidates SET state='failed',error_code=p_error,materialize_lease_until=clock_timestamp() WHERE id=c.id;RETURN true;
 END IF;
 IF p_hash !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_config)<>'object' OR octet_length(p_config::text)>110000000 OR jsonb_typeof(p_metrics)<>'object'
 OR p_config->>'schema_version'<>'2.0.0' OR p_config->>'renderer_version'<>'photo_renderer_v2' OR p_config->>'style_id' IS NULL OR p_low IS NULL THEN
  RAISE EXCEPTION 'invalid candidate' USING ERRCODE='22023';
 END IF;
 UPDATE photo.style_candidates SET state='ready',config_json=p_config,config_hash=p_hash,metrics=p_metrics,low_confidence=p_low,materialize_lease_until=clock_timestamp(),error_code=NULL WHERE id=c.id;
 UPDATE photo.runs SET status='waiting_review',revision=revision+1 WHERE tenant_id=c.tenant_id AND id=c.training_run_id AND status NOT IN ('cancelled','failed');
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_style_candidate(uuid,uuid,jsonb,text,jsonb,boolean,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_style_candidate(uuid,uuid,jsonb,text,jsonb,boolean,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_style_candidate(uuid,uuid,jsonb,text,jsonb,boolean,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
