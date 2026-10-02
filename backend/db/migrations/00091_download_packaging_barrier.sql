-- +goose Up
CREATE TABLE photo.download_build_state (
 tenant_id uuid NOT NULL,run_id uuid NOT NULL,phase text NOT NULL CHECK(phase IN ('planning','encoding','packaging','archiving')),
 item_count bigint NOT NULL CHECK(item_count BETWEEN 1 AND 5000),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,run_id),FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id)
);
ALTER TABLE photo.download_build_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.download_build_state FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.download_build_state TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE ON photo.download_build_state TO photo_fn_owner;

-- Existing active runs from SQL90 are recoverable after rolling migration.
INSERT INTO photo.download_build_state(tenant_id,run_id,phase,item_count)
 SELECT r.tenant_id,r.id,CASE WHEN r.graph_sealed THEN 'encoding' ELSE 'planning' END,s.item_count
 FROM photo.runs r JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 JOIN photo.download_jobs j ON j.tenant_id=r.tenant_id AND j.run_id=r.id
 WHERE r.kind='download' AND r.status IN ('queued','running') AND NOT r.cancel_requested AND j.status IN ('queued','preparing','building')
 AND NOT j.cancel_requested AND s.kind='selection' AND s.item_count BETWEEN 1 AND 5000 ON CONFLICT DO NOTHING;

-- +goose StatementBegin
CREATE FUNCTION photo.init_download_build_state() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE count_value bigint;
BEGIN
 SELECT item_count INTO count_value FROM photo.snapshots WHERE tenant_id=NEW.tenant_id AND id=(SELECT input_snapshot_id FROM photo.runs WHERE tenant_id=NEW.tenant_id AND id=NEW.run_id) AND kind='selection';
 IF count_value NOT BETWEEN 1 AND 5000 THEN RAISE EXCEPTION 'DOWNLOAD_SNAPSHOT_INVALID' USING ERRCODE='23514';END IF;
 INSERT INTO photo.download_build_state(tenant_id,run_id,phase,item_count) VALUES(NEW.tenant_id,NEW.run_id,'planning',count_value) ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.init_download_build_state() OWNER TO photo_fn_owner;
CREATE TRIGGER init_download_build_state AFTER INSERT ON photo.download_jobs FOR EACH ROW EXECUTE FUNCTION photo.init_download_build_state();

-- +goose StatementBegin
CREATE FUNCTION photo.download_build_phase(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT b.phase FROM photo.download_build_state b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 JOIN photo.download_jobs j ON j.tenant_id=r.tenant_id AND j.run_id=r.id
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND r.kind='download' AND r.status IN ('queued','running')
 AND NOT r.cancel_requested AND NOT j.cancel_requested AND j.status IN ('queued','preparing','building')
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.download_build_phase(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_build_phase(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.download_build_phase(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_download_build()
RETURNS TABLE(tenant_id uuid,run_id uuid,lease_token uuid,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;token uuid:=gen_random_uuid();until_time timestamptz;
BEGIN
 SELECT rr.* INTO r FROM photo.runs rr JOIN photo.download_jobs j ON j.tenant_id=rr.tenant_id AND j.run_id=rr.id
 JOIN photo.download_build_state b ON b.tenant_id=rr.tenant_id AND b.run_id=rr.id
 WHERE rr.kind='download' AND rr.status IN ('queued','running') AND NOT rr.cancel_requested AND NOT j.cancel_requested
 AND rr.next_download_build_at<=clock_timestamp() AND (rr.download_build_until IS NULL OR rr.download_build_until<=clock_timestamp())
 AND ((b.phase='planning' AND NOT rr.graph_sealed AND j.status IN ('queued','preparing'))
   OR (b.phase='encoding' AND rr.graph_sealed AND j.status='building' AND NOT EXISTS(
    SELECT FROM photo.tasks t WHERE t.tenant_id=rr.tenant_id AND t.run_id=rr.id AND t.operation='export.encode' AND t.state NOT IN ('succeeded','failed','cancelled','skipped'))))
 ORDER BY rr.next_download_build_at,rr.id LIMIT 1 FOR UPDATE OF rr SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 UPDATE photo.runs SET download_build_token=token,download_build_until=until_time WHERE id=r.id;
 UPDATE photo.download_jobs SET status='preparing' WHERE tenant_id=r.tenant_id AND run_id=r.id AND status='queued';
 RETURN QUERY SELECT r.tenant_id,r.id,token,until_time;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_download_build() OWNER TO photo_fn_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.renew_download_build(p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 UPDATE photo.runs r SET download_build_until=clock_timestamp()+interval '60 seconds'
 FROM photo.download_build_state b WHERE r.id=p_run AND r.kind='download' AND r.status IN ('queued','running')
 AND NOT r.cancel_requested AND r.download_build_token=p_token AND r.download_build_until>clock_timestamp()
 AND b.tenant_id=r.tenant_id AND b.run_id=r.id AND b.phase IN ('planning','encoding');
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_download_build(uuid,uuid) OWNER TO photo_fn_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.seal_download_exports(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;j photo.download_jobs%ROWTYPE;b photo.download_build_state%ROWTYPE;expected bigint;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO j FROM photo.download_jobs WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO b FROM photo.download_build_state WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR j.id IS NULL OR b.run_id IS NULL OR r.kind<>'download' OR r.cancel_requested OR j.cancel_requested OR r.status NOT IN ('queued','running') THEN RETURN false;END IF;
 IF b.phase='encoding' THEN RETURN r.graph_sealed;END IF;
 IF b.phase<>'planning' OR r.graph_sealed THEN RETURN false;END IF;
 expected:=b.item_count;
 IF expected NOT BETWEEN 1 AND 5000 OR (SELECT count(*) FROM photo.download_export_plans WHERE tenant_id=p_tenant AND run_id=p_run)<>expected
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='export.encode')<>expected
 OR EXISTS(SELECT FROM photo.download_export_plans p LEFT JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id
  WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND (t.id IS NULL OR t.state<>'blocked' OR t.inputs<>p.inputs)) THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='ready',next_attempt_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run AND operation='export.encode' AND state='blocked';
 UPDATE photo.download_build_state SET phase='encoding',updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1,download_build_token=NULL,download_build_until=NULL WHERE tenant_id=p_tenant AND id=p_run;
 UPDATE photo.download_jobs SET status='building' WHERE tenant_id=p_tenant AND id=j.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_download_exports(uuid,uuid) OWNER TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.download_encode_outcomes(p_tenant uuid,p_run uuid)
RETURNS TABLE(ordinal bigint,asset_version_id uuid,state text,artifact_id uuid,sha256 text,bytes bigint,filename text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT p.ordinal,si.asset_version_id,t.state,a.id,a.sha256,a.bytes,
  regexp_replace(coalesce(nullif(si.payload->>'filename',''),u.filename,'photo-'||lpad(p.ordinal::text,4,'0')),'\.[^.]*$','')||
  CASE j.preset->'config'->>'format' WHEN 'jpeg' THEN '.jpg' WHEN 'png' THEN '.png' WHEN 'tiff' THEN '.tiff' END
 FROM photo.download_build_state b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 JOIN photo.download_jobs j ON j.tenant_id=r.tenant_id AND j.run_id=r.id
 JOIN photo.download_export_plans p ON p.tenant_id=r.tenant_id AND p.run_id=r.id
 JOIN photo.snapshot_items si ON si.tenant_id=r.tenant_id AND si.snapshot_id=r.input_snapshot_id AND si.ordinal=p.ordinal
 JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id
 JOIN photo.assets source ON source.tenant_id=v.tenant_id AND source.id=v.asset_id
 LEFT JOIN photo.upload_sessions u ON u.tenant_id=source.tenant_id AND u.id=CASE WHEN source.provenance->>'upload_id' ~ '^[0-9a-f-]{36}$' THEN (source.provenance->>'upload_id')::uuid END
 JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id AND t.run_id=p.run_id AND t.operation='export.encode'
 LEFT JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id AND t.state='succeeded'
 LEFT JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.output_role='encoded_image' AND a.state='ready'
  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.phase='encoding' AND r.graph_sealed AND r.status='running' AND NOT r.cancel_requested
 AND j.status='building' AND NOT j.cancel_requested AND (a.id IS NULL OR (a.bytes>0 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')))
 ORDER BY p.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.download_encode_outcomes(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_encode_outcomes(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.download_encode_outcomes(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.begin_download_packaging(p_tenant uuid,p_run uuid,p_plan jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;j photo.download_jobs%ROWTYPE;b photo.download_build_state%ROWTYPE;count_value bigint;entry jsonb;actual record;
BEGIN
 IF p_plan IS NULL OR jsonb_typeof(p_plan)<>'array' OR jsonb_array_length(p_plan) NOT BETWEEN 1 AND 5000 OR octet_length(p_plan::text)>8388608 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO j FROM photo.download_jobs WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO b FROM photo.download_build_state WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR j.id IS NULL OR b.run_id IS NULL OR b.phase<>'encoding' OR NOT r.graph_sealed OR r.cancel_requested OR j.cancel_requested THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='export.encode' AND state<>'succeeded') THEN RETURN false;END IF;
 count_value:=jsonb_array_length(p_plan);IF count_value<>b.item_count THEN RETURN false;END IF;

 IF EXISTS(SELECT FROM jsonb_array_elements(p_plan) x GROUP BY (x->>'volume_no')::integer
  HAVING count(*) FILTER(WHERE x->>'delivery_kind'='single') NOT IN (0,1)
  OR (count(*) FILTER(WHERE x->>'delivery_kind'='single')=1 AND count(*)<>1)
  OR sum((x->>'bytes')::bigint) FILTER(WHERE x->>'delivery_kind'='zip')>10737418240)
 OR (SELECT count(DISTINCT (x->>'volume_no')::integer) FROM jsonb_array_elements(p_plan) x)<>
    (SELECT max((x->>'volume_no')::integer) FROM jsonb_array_elements(p_plan) x)
 OR (SELECT count(*) FROM (SELECT lower(x->>'entry_name') FROM jsonb_array_elements(p_plan) x GROUP BY lower(x->>'entry_name') HAVING count(*)>1) duplicate)>0 THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_plan) LOOP
  IF (entry-'ordinal'-'asset_version_id'-'artifact_id'-'sha256'-'bytes'-'filename'-'entry_name'-'volume_no'-'delivery_kind')<>'{}'
  OR (entry->>'ordinal')::bigint NOT BETWEEN 0 AND b.item_count-1 OR (entry->>'volume_no')::integer NOT BETWEEN 1 AND 5000
  OR entry->>'delivery_kind' NOT IN ('zip','single') OR octet_length(entry->>'entry_name') NOT BETWEEN 1 AND 180 THEN RETURN false;END IF;
  SELECT * INTO actual FROM photo.download_encode_outcomes(p_tenant,p_run) o WHERE o.ordinal=(entry->>'ordinal')::bigint;
  IF NOT FOUND OR actual.state<>'succeeded' OR actual.asset_version_id<>(entry->>'asset_version_id')::uuid OR actual.artifact_id<>(entry->>'artifact_id')::uuid
  OR actual.sha256<>entry->>'sha256' OR actual.bytes<>(entry->>'bytes')::bigint OR actual.filename<>entry->>'filename' THEN RETURN false;END IF;
 END LOOP;
 IF EXISTS(SELECT FROM photo.download_job_items WHERE tenant_id=p_tenant AND download_job_id=j.id) THEN
  RETURN b.phase IN ('packaging','archiving');END IF;
 INSERT INTO photo.download_job_items(tenant_id,download_job_id,ordinal,asset_version_id,artifact_id,sha256,entry_name,volume_no,bytes,delivery_kind)
 SELECT p_tenant,j.id,(x->>'ordinal')::bigint,(x->>'asset_version_id')::uuid,(x->>'artifact_id')::uuid,x->>'sha256',x->>'entry_name',(x->>'volume_no')::integer,(x->>'bytes')::bigint,x->>'delivery_kind'
 FROM jsonb_array_elements(p_plan) x ORDER BY (x->>'ordinal')::bigint;
 INSERT INTO photo.download_volumes(tenant_id,download_job_id,volume_no,artifact_id,status,files_done,bytes_written)
 SELECT p_tenant,j.id,(x->>'volume_no')::integer,
  CASE WHEN bool_and(x->>'delivery_kind'='single') THEN (array_agg((x->>'artifact_id')::uuid))[1] END,
  CASE WHEN bool_and(x->>'delivery_kind'='single') THEN 'ready' ELSE 'queued' END,
  CASE WHEN bool_and(x->>'delivery_kind'='single') THEN count(*) ELSE 0 END,
  CASE WHEN bool_and(x->>'delivery_kind'='single') THEN sum((x->>'bytes')::bigint) ELSE 0 END
 FROM jsonb_array_elements(p_plan) x GROUP BY (x->>'volume_no')::integer ORDER BY (x->>'volume_no')::integer;
 UPDATE photo.download_build_state SET phase='packaging',updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET graph_sealed=false,revision=revision+1,download_build_token=NULL,download_build_until=NULL WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_download_packaging(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_download_packaging(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_download_packaging(uuid,uuid,jsonb) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
