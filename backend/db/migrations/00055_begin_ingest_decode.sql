-- +goose Up
ALTER TABLE photo.ingest_progress DROP CONSTRAINT ingest_progress_phase_check;
ALTER TABLE photo.ingest_progress ADD CONSTRAINT ingest_progress_phase_check CHECK(phase IN ('inspection','decode_build','decode','catalog','complete'));
ALTER TABLE photo.ingest_progress ADD COLUMN decode_manifest jsonb;
CREATE TABLE photo.ingest_decode_items (
 tenant_id uuid NOT NULL,run_id uuid NOT NULL,ordinal bigint NOT NULL,
 outcome text NOT NULL CHECK(outcome IN ('decode','failed')),
 metadata_id uuid,metadata_sha text,failure_stage text,failure_reason text,
 PRIMARY KEY(tenant_id,run_id,ordinal),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),
 FOREIGN KEY(tenant_id,metadata_id) REFERENCES photo.artifacts(tenant_id,id),
 CHECK((outcome='decode' AND metadata_id IS NOT NULL AND metadata_sha IS NOT NULL AND metadata_sha ~ '^[0-9a-f]{64}$' AND failure_stage IS NULL AND failure_reason IS NULL)
 OR (outcome='failed' AND metadata_id IS NULL AND metadata_sha IS NULL AND failure_stage IS NOT NULL AND failure_reason IS NOT NULL AND failure_stage IN ('asset.verify','asset.inspect') AND failure_reason IN ('failed','cancelled')))
);
ALTER TABLE photo.ingest_decode_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.ingest_decode_items FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.ingest_decode_items TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.ingest_decode_items TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.begin_ingest_decode(p_tenant uuid,p_run uuid,p_metadata jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;p photo.ingest_progress%ROWTYPE;o record;entry jsonb;recorded jsonb:='[]';
 seen integer:=0;successful integer:=0;stage text;reason text;
BEGIN
 IF p_metadata IS NULL OR jsonb_typeof(p_metadata) IS DISTINCT FROM 'array' OR jsonb_array_length(p_metadata)>10000 OR octet_length(p_metadata::text)>4194304 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR r.status NOT IN ('queued','running') OR r.revision>=9007199254740991 THEN RETURN false;END IF;
 SELECT * INTO p FROM photo.ingest_progress WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF p.phase<>'inspection' THEN RETURN coalesce(p.decode_manifest=p_metadata,false);END IF;
 IF NOT r.graph_sealed THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_metadata) LOOP
  IF jsonb_typeof(entry) IS DISTINCT FROM 'object' OR (entry-'ordinal'-'artifact_id'-'sha256')<>'{}'
  OR jsonb_typeof(entry->'ordinal') IS DISTINCT FROM 'number' OR (entry->>'ordinal')::numeric<>trunc((entry->>'ordinal')::numeric)
  OR entry->>'artifact_id' IS NULL OR entry->>'sha256' IS NULL OR entry->>'sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT count(DISTINCT value->>'ordinal') FROM jsonb_array_elements(p_metadata))<>jsonb_array_length(p_metadata) THEN RETURN false;END IF;
 FOR o IN SELECT * FROM photo.ingest_inspection_outcomes(p_tenant,p_run) LOOP
  seen:=seen+1;stage:=NULL;reason:=NULL;
  IF o.verify_state IN ('failed','cancelled') AND o.inspect_state='skipped' AND o.inspect_reason='upstream_failed' THEN stage:='asset.verify';reason:=o.verify_state;
  ELSIF o.verify_state='succeeded' AND o.inspect_state IN ('failed','cancelled') THEN stage:='asset.inspect';reason:=o.inspect_state;
  ELSIF o.verify_state='succeeded' AND o.inspect_state='succeeded' THEN
   SELECT value INTO entry FROM jsonb_array_elements(p_metadata) WHERE (value->>'ordinal')::numeric=o.ordinal;
   IF NOT FOUND OR o.metadata IS NULL OR entry->>'artifact_id' IS DISTINCT FROM o.metadata->>'artifact_id'
   OR entry->>'sha256' IS DISTINCT FROM o.metadata->>'sha256' THEN RETURN false;END IF;
   successful:=successful+1;
   recorded:=recorded||jsonb_build_array(jsonb_build_object('ordinal',o.ordinal,'outcome','decode','metadata_id',entry->>'artifact_id','metadata_sha',entry->>'sha256'));
  ELSE RETURN false;
  END IF;
  IF stage IS NOT NULL THEN recorded:=recorded||jsonb_build_array(jsonb_build_object('ordinal',o.ordinal,'outcome','failed','stage',stage,'reason',reason));END IF;
 END LOOP;
 IF seen<>p.item_count OR successful<>jsonb_array_length(p_metadata) THEN RETURN false;END IF;
 -- No mutation above this line: an invalid later item cannot leave a partial batch.
 INSERT INTO photo.ingest_decode_items(tenant_id,run_id,ordinal,outcome,metadata_id,metadata_sha,failure_stage,failure_reason)
 SELECT p_tenant,p_run,(value->>'ordinal')::bigint,value->>'outcome',(value->>'metadata_id')::uuid,value->>'metadata_sha',value->>'stage',value->>'reason'
 FROM jsonb_array_elements(recorded);
 UPDATE photo.ingest_progress SET phase='decode_build',decode_manifest=p_metadata WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET graph_sealed=false,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_ingest_decode(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_ingest_decode(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_ingest_decode(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
