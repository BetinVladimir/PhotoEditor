-- +goose Up
-- +goose StatementBegin
-- Internal failure transition. Callers hold run→job and have already established
-- a terminal, invalid producer tuple. No runtime role can invoke this directly.
CREATE FUNCTION photo.fail_download_output_locked(p_tenant uuid,p_run uuid,p_volumes integer[]) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;j photo.download_jobs%ROWTYPE;report jsonb;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO j FROM photo.download_jobs WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR j.id IS NULL OR r.kind<>'download' OR r.status NOT IN('queued','running') OR r.cancel_requested OR j.cancel_requested
 OR j.status NOT IN('building','preparing') OR p_volumes IS NULL OR cardinality(p_volumes)<1 THEN RETURN false;END IF;
 -- A retried generation is still work in progress; never terminalize its parent
 -- on the basis of a formerly succeeded result that has become nonterminal.
 IF EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
  AND t.operation IN('archive.build','export.encode') AND t.state NOT IN('succeeded','failed','cancelled','skipped')) THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('ordinal',i.ordinal,'asset_version_id',i.asset_version_id,'code',
  CASE i.delivery_kind WHEN 'single' THEN 'EXPORT_OUTPUT_UNAVAILABLE' ELSE 'ARCHIVE_OUTPUT_UNAVAILABLE' END) ORDER BY i.ordinal)
 INTO report FROM photo.download_job_items i WHERE i.tenant_id=p_tenant AND i.download_job_id=j.id AND i.volume_no=ANY(p_volumes);
 IF report IS NULL THEN RETURN false;END IF;
 UPDATE photo.download_jobs SET status='failed',failure=jsonb_build_object('code','DOWNLOAD_ARCHIVE_FAILED','items',report) WHERE tenant_id=p_tenant AND id=j.id;
 UPDATE photo.runs SET status='failed',revision=revision+1,download_build_token=NULL,download_build_until=NULL WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
ALTER FUNCTION photo.fail_download_output_locked(uuid,uuid,integer[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_download_output_locked(uuid,uuid,integer[]) FROM PUBLIC,photo_scheduler,photo_api,photo_portal,photo_worker;
DO $$
DECLARE source text;anchor text;replacement text;
BEGIN
 -- Guest cancel identifies the run without taking the job lock; after run lock,
 -- repeat the exact authorized job lookup and check terminal state again.
 source:=pg_get_functiondef('photo.cancel_portal_download(bytea,bytea,uuid)'::regprocedure);
 anchor:=$a$AND x.policy_version=ident.link_version FOR UPDATE;$a$;
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'DOWNLOAD_CANCEL_LOCK_SOURCE';END IF;
 source:=replace(source,anchor,'AND x.policy_version=ident.link_version;');
 anchor:=$a$IF j.status IN ('queued','preparing','building','uploading') THEN$a$;
 replacement:=$a$SELECT * INTO j FROM photo.download_jobs x WHERE x.tenant_id=r.tenant_id AND x.id=p_job AND x.run_id=r.id
 AND x.snapshot_id=r.input_snapshot_id AND x.share_id=ident.share_id AND x.principal_type='guest' AND x.principal_id=ident.session_id
 AND x.policy_version=ident.link_version FOR UPDATE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'not_found',NULL::uuid,NULL::text,NULL::bigint,NULL::bigint,NULL::bigint,NULL::jsonb,NULL::timestamptz,NULL::jsonb;RETURN;END IF;
 IF r.status IN ('draft','queued','running','waiting_review') AND j.status IN ('queued','preparing','building','uploading') THEN$a$;
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'DOWNLOAD_CANCEL_STATE_SOURCE';END IF;
 source:=replace(source,anchor,replacement);
 source:=replace(source,'j.expires_at,NULL::jsonb;','j.expires_at,j.failure;');
 EXECUTE source;
 -- Do not silently wait forever after a terminal archive lost its exact outputs.
 source:=pg_get_functiondef('photo.finalize_download_archives(uuid,uuid)'::regprocedure);
 source:=replace(source,$source$a.created_by_task_id=t.id AND a.output_role=$source$,$source$a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token AND a.output_role=$source$);
 anchor:=$a$OR manifest.bytes<1 OR manifest.object_version IS NULL OR manifest.object_version IN ('','null') THEN RETURN false;END IF;$a$;
 replacement:=$a$OR manifest.bytes<1 OR manifest.object_version IS NULL OR manifest.object_version IN ('','null') THEN RETURN photo.fail_download_output_locked(p_tenant,p_run,ARRAY[v.volume_no]);END IF;$a$;
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'DOWNLOAD_ARCHIVE_UNAVAILABLE_SOURCE';END IF;
 EXECUTE replace(source,anchor,replacement);
 -- The direct-only path has no archive worker to produce a failure receipt.
 -- Preserve its current exact producer check and turn only that proven invalid
 -- terminal inventory into a safe failed job, before any publication.
 source:=pg_get_functiondef('photo.seal_download_archives(uuid,uuid)'::regprocedure);
 source:=replace(source,$source$AND artifact.state='ready' AND artifact.output_role='encoded_image'$source$,$source$AND artifact.state='ready' AND artifact.output_role='encoded_image' AND artifact.bytes>0 AND artifact.sha256 ~ '^[0-9a-f]{64}$' AND artifact.produced_attempt=result.attempt AND artifact.produced_fence=result.fencing_token$source$);
 anchor:=$a$FOR SHARE OF artifact,task,result)) THEN RETURN false;END IF;$a$;
 replacement:=$a$FOR SHARE OF artifact,task,result)) THEN
 RETURN photo.fail_download_output_locked(p_tenant,p_run,ARRAY(SELECT volume.volume_no FROM photo.download_volumes volume
  WHERE volume.tenant_id=p_tenant AND volume.download_job_id=j.id AND volume.status='ready'
  AND NOT EXISTS(SELECT FROM photo.artifacts artifact JOIN photo.tasks task ON task.tenant_id=artifact.tenant_id AND task.id=artifact.created_by_task_id
   JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.task_id=task.id AND result.id=task.canonical_result_id
   AND result.attempt=task.attempt AND result.fencing_token=task.fencing_token
   WHERE artifact.tenant_id=volume.tenant_id AND artifact.id=volume.artifact_id AND artifact.state='ready' AND artifact.output_role='encoded_image' AND artifact.bytes>0 AND artifact.sha256 ~ '^[0-9a-f]{64}$' AND artifact.produced_attempt=result.attempt AND artifact.produced_fence=result.fencing_token
   AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp()) AND artifact.object_version IS NOT NULL AND artifact.object_version NOT IN ('','null')
   AND task.run_id=p_run AND task.operation='export.encode' AND task.state='succeeded'
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',artifact.id::text,'sha256',artifact.sha256)))));
 END IF;$a$;
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'DOWNLOAD_DIRECT_UNAVAILABLE_SOURCE';END IF;
 EXECUTE replace(source,anchor,replacement);
 -- Every installed single-image producer uses its declared output policies,
 -- not an obsolete manual list. Master and nested arrays must belong to the
 -- same current attempt/fence and exact canonical result. V2 normalization is
 -- handled separately; this accessor remains a WorkingImageV1 contract.
 source:=pg_get_functiondef('photo.download_export_inputs(uuid,uuid)'::regprocedure);
 anchor:=$a$t.operation IN ('asset.decode','image.denoise','image.color','image.local','portrait.beauty','portrait.detail','defects.repair','background.compose','background.blur','background.sky','image.upscale','hdr.merge','hdr.window_pull','derivative.resize','derivative.sharpen','derivative.watermark')$a$;
 replacement:=$a$EXISTS(SELECT FROM photo.operation_output_policies policy WHERE policy.operation=t.operation AND policy.version=t.operation_version
 AND policy.role='working_image' AND policy.media_types ? 'application/json')
 AND EXISTS(SELECT FROM photo.operation_output_policies policy WHERE policy.operation=t.operation AND policy.version=t.operation_version
 AND policy.role='working_rgb' AND policy.media_types ? 'application/octet-stream')$a$;
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'DOWNLOAD_MASTER_PRODUCER_SOURCE';END IF;
 source:=replace(source,anchor,replacement);
 anchor:='z.id=t.canonical_result_id';
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'DOWNLOAD_MASTER_ATTEMPT_SOURCE';END IF;
 source:=replace(source,anchor,anchor||' AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token');
 source:=replace(source,$source$AND master.state='ready' AND master.output_role='working_image'$source$, $source$AND master.state='ready' AND master.output_role='working_image' AND master.produced_attempt=z.attempt AND master.produced_fence=z.fencing_token$source$);
 source:=replace(source,$source$AND a.state='ready' AND a.object_version$source$, $source$AND a.state='ready' AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token AND a.object_version$source$);
 EXECUTE source;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
-- Exact physical WorkingImage refs, derived by the coordinator from the frozen
-- master bytes. Modern render can retain its declared source RGB/alpha unchanged;
-- an unchanged ref is not required to be a newly produced sibling artifact.
CREATE FUNCTION photo.download_export_bound_input(p_tenant uuid,p_run uuid,p_ordinal bigint,p_master uuid,p_master_sha text,p_input uuid,p_input_sha text,p_role text)
RETURNS TABLE(ordinal bigint,role text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT si.ordinal,p_role,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.runs r JOIN photo.download_jobs j ON j.tenant_id=r.tenant_id AND j.run_id=r.id AND j.snapshot_id=r.input_snapshot_id
 JOIN photo.snapshot_items si ON si.tenant_id=r.tenant_id AND si.snapshot_id=r.input_snapshot_id AND si.ordinal=p_ordinal
 JOIN photo.artifacts master ON master.tenant_id=si.tenant_id AND master.id=si.artifact_id AND master.sha256=si.sha256
 JOIN photo.tasks t ON t.tenant_id=master.tenant_id AND t.id=master.created_by_task_id AND t.state='succeeded'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
  AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=master.tenant_id AND a.id=p_input AND a.sha256=p_input_sha
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='download' AND r.status IN('queued','running') AND NOT r.cancel_requested
 AND NOT j.cancel_requested AND j.status IN('queued','preparing') AND master.id=p_master AND master.sha256=p_master_sha
 AND master.state='ready' AND master.output_role='working_image' AND master.produced_attempt=z.attempt AND master.produced_fence=z.fencing_token
 AND (master.expires_at IS NULL OR master.expires_at>clock_timestamp())
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',master.id::text,'sha256',master.sha256))
 AND EXISTS(SELECT FROM photo.operation_output_policies policy WHERE policy.operation=t.operation AND policy.version=t.operation_version AND policy.role='working_image' AND policy.media_types ? 'application/json')
 AND ((a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256)))
  OR t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
  OR EXISTS(SELECT FROM photo.v2_task_inputs input JOIN photo.v2_workflows workflow ON workflow.tenant_id=input.tenant_id AND workflow.run_id=t.run_id AND workflow.project_id=input.project_id
   WHERE input.tenant_id=t.tenant_id AND input.task_id=t.id AND input.artifact_id=a.id AND input.sha256=a.sha256 AND input.contract='NpyArray'))
 AND ((p_role='working_rgb' AND a.output_role IN('working_rgb','pixels')) OR (p_role='alpha' AND a.output_role IN('alpha','working_alpha')))
 AND a.state='ready' AND a.bytes>0 AND a.bytes<=2401048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.media_type IN('application/octet-stream','application/x-npy') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
$$;
ALTER FUNCTION photo.download_export_bound_input(uuid,uuid,bigint,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_export_bound_input(uuid,uuid,bigint,uuid,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.download_export_bound_input(uuid,uuid,bigint,uuid,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.fail_download_planning_source(p_tenant uuid,p_run uuid,p_ordinal bigint DEFAULT NULL,p_master uuid DEFAULT NULL,p_sha text DEFAULT NULL,p_code text DEFAULT NULL) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;j photo.download_jobs%ROWTYPE;b photo.download_build_state%ROWTYPE;report jsonb;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO j FROM photo.download_jobs WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO b FROM photo.download_build_state WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR j.id IS NULL OR b.run_id IS NULL OR r.kind<>'download' OR b.phase<>'planning' OR r.status NOT IN('queued','running') OR r.cancel_requested OR j.cancel_requested OR j.status NOT IN('queued','preparing') THEN RETURN false;END IF;
 IF p_ordinal IS NOT NULL THEN
  -- Only the trusted scheduler's closed physical observation of the exact
  -- frozen source is accepted; arbitrary IO errors never reach this transition.
  IF p_code IS NULL OR p_code NOT IN('ARTIFACT_MISSING','ARTIFACT_HASH_MISMATCH','SOURCE_INPUT_INVALID','SOURCE_OUTPUT_UNAVAILABLE')
   OR NOT EXISTS(SELECT FROM photo.snapshot_items si WHERE si.tenant_id=p_tenant AND si.snapshot_id=j.snapshot_id AND si.ordinal=p_ordinal AND si.artifact_id=p_master AND si.sha256=p_sha) THEN RETURN false;END IF;
  IF EXISTS(SELECT FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
   WHERE a.tenant_id=p_tenant AND a.id=p_master AND t.state NOT IN('succeeded','failed','cancelled','skipped')) THEN RETURN false;END IF;
  SELECT jsonb_agg(jsonb_build_object('ordinal',si.ordinal,'asset_version_id',si.asset_version_id,'code',p_code) ORDER BY si.ordinal) INTO report
  FROM photo.snapshot_items si WHERE si.tenant_id=p_tenant AND si.snapshot_id=j.snapshot_id AND si.ordinal=p_ordinal;
 ELSE
  -- Catalog loss is independently established, rather than inferred from an
  -- empty/transient result set. Accepted selection expiry does not revoke pins.
  SELECT jsonb_agg(jsonb_build_object('ordinal',si.ordinal,'asset_version_id',si.asset_version_id,'code','SOURCE_OUTPUT_UNAVAILABLE') ORDER BY si.ordinal) INTO report
  FROM photo.snapshot_items si LEFT JOIN photo.artifacts a ON a.tenant_id=si.tenant_id AND a.id=si.artifact_id AND a.sha256=si.sha256
   AND a.state='ready' AND a.output_role='working_image' AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  WHERE si.tenant_id=p_tenant AND si.snapshot_id=j.snapshot_id AND a.id IS NULL
  AND NOT EXISTS(SELECT FROM photo.artifacts original JOIN photo.tasks producer ON producer.tenant_id=original.tenant_id AND producer.id=original.created_by_task_id WHERE original.tenant_id=si.tenant_id AND original.id=si.artifact_id AND producer.state NOT IN('succeeded','failed','cancelled','skipped'));
 END IF;
 IF report IS NULL THEN RETURN false;END IF;
 UPDATE photo.download_jobs SET status='failed',failure=jsonb_build_object('code','DOWNLOAD_EXPORT_FAILED','items',report) WHERE tenant_id=p_tenant AND id=j.id;
 UPDATE photo.runs SET status='failed',revision=revision+1,download_build_token=NULL,download_build_until=NULL WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
ALTER FUNCTION photo.fail_download_planning_source(uuid,uuid,bigint,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_download_planning_source(uuid,uuid,bigint,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.fail_download_planning_source(uuid,uuid,bigint,uuid,text,text) TO photo_scheduler;
DO $$ DECLARE source text;anchor text;BEGIN
 source:=pg_get_functiondef('photo.download_export_context(uuid,uuid)'::regprocedure);
 anchor:=' AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp())';
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'DOWNLOAD_ACCEPTED_SNAPSHOT_EXPIRY_SOURCE';END IF;
 EXECUTE replace(source,anchor,'');
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
-- Legacy Download is an encode/ZIP endpoint. Rich presentation is admitted by
-- Delivery; never accept a legacy job that its compiler cannot execute.
CREATE FUNCTION photo.download_preset_compatible(p jsonb) RETURNS boolean LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(jsonb_typeof(p)='object' AND p->>'format' IN('jpeg','png','tiff') AND p->>'icc_id' IN('srgb_v1','work_rec2020_gamma24_v1')
 AND p->>'subsampling'='444' AND p->>'alpha' IN('preserve','matte') AND p->>'metadata_policy'='strip'
 AND (p->'watermark' IS NULL OR p->'watermark'='null'::jsonb) AND (p->'sharpen' IS NULL OR p->'sharpen'='null'::jsonb)
 AND (p->'size' IS NULL OR p->'size'='null'::jsonb OR p#>>'{size,fit}'='contain'),false)
$$;
ALTER FUNCTION photo.download_preset_compatible(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_preset_compatible(jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.download_preset_compatible(jsonb) TO photo_api;
DO $$ DECLARE source text;anchor text;BEGIN
 source:=pg_get_functiondef('photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid)'::regprocedure);
 anchor:=' SELECT created_by INTO creator FROM photo.share_links';
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'DOWNLOAD_PRESET_ADMISSION_SOURCE';END IF;
 EXECUTE replace(source,anchor,$a$ IF photo.download_preset_compatible(pr.config) IS DISTINCT FROM true THEN RETURN QUERY SELECT 'preset_invalid',NULL::uuid,NULL::text;RETURN;END IF;
 SELECT created_by INTO creator FROM photo.share_links$a$);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
-- Keep terminal diagnostics while releasing expired temporary object pins.
CREATE FUNCTION photo.expire_terminal_download_pins() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.status IN ('failed','cancelled') THEN
  NEW.expires_at:=least(coalesce(NEW.expires_at,clock_timestamp()),clock_timestamp());
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.expire_terminal_download_pins() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.expire_terminal_download_pins() FROM PUBLIC;
CREATE TRIGGER expire_terminal_download_pins BEFORE INSERT OR UPDATE OF status ON photo.download_jobs
FOR EACH ROW EXECUTE FUNCTION photo.expire_terminal_download_pins();
UPDATE photo.download_jobs SET expires_at=clock_timestamp() WHERE status IN ('failed','cancelled') AND expires_at IS NULL;
DO $$ DECLARE source text;anchor text;BEGIN
 source:=pg_get_functiondef('photo.claim_artifact_gc()'::regprocedure);
 anchor:='UPDATE photo.download_jobs j SET status=''expired'' WHERE j.tenant_id=a.tenant_id AND j.expires_at<=clock_timestamp()';
 IF strpos(source,anchor)=0 THEN RAISE EXCEPTION 'DOWNLOAD_GC_TERMINAL_STATUS_SOURCE';END IF;
 source:=replace(source,anchor,anchor||' AND j.status=''ready''');
 anchor:='AND j.status=''expired'')';
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>2 THEN RAISE EXCEPTION 'DOWNLOAD_GC_TERMINAL_DETACH_SOURCE';END IF;
 source:=replace(source,anchor,'AND j.status IN (''expired'',''failed'',''cancelled'') AND j.expires_at<=clock_timestamp())');
 EXECUTE source;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
