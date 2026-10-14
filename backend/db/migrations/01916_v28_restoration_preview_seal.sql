-- +goose Up
-- Private post-IO witness check; does not activate the preview graph.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_preview_arrays_current(p_tenant uuid,p_run uuid,
 p_before text,p_after text,p_bucket text,p_arrays jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;s record; before_image jsonb;after_image jsonb;
 item jsonb;expected jsonb;locator jsonb;image text;side text;channel text;
 total integer;ordinal integer:=0;has_alpha boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_run IS NULL OR p_before IS NULL OR octet_length(p_before) NOT BETWEEN 2 AND 1048576
 OR p_after IS NULL OR octet_length(p_after) NOT BETWEEN 2 AND 1048576
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
 OR jsonb_typeof(p_arrays) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'preview' OR r.v28_restoration_preview_task_id IS NULL
 OR r.cancel_requested OR r.status NOT IN('draft','running','completed') THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.v28_restoration_preview_source(p_tenant,r.v28_restoration_preview_task_id);
 IF NOT FOUND OR r.config_snapshot->>'source_review_id' IS DISTINCT FROM s.review_id::text
 OR r.config_snapshot->>'source_result_id' IS DISTINCT FROM s.result_id::text
 OR r.config_snapshot->>'source_attempt' IS DISTINCT FROM s.attempt::text
 OR r.config_snapshot->>'source_fence' IS DISTINCT FROM s.fence::text
 OR r.config_snapshot->'root' IS DISTINCT FROM jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
 OR encode(sha256(convert_to(p_before,'UTF8')),'hex') IS DISTINCT FROM s.source_sha
 OR encode(sha256(convert_to(p_after,'UTF8')),'hex') IS DISTINCT FROM s.candidate_sha
 THEN RETURN false;END IF;
 before_image:=p_before::jsonb;after_image:=p_after::jsonb;
 IF jsonb_typeof(before_image)<>'object' OR jsonb_typeof(after_image)<>'object'
 OR before_image->>'dtype' IS DISTINCT FROM 'float32'
 OR after_image->>'dtype' IS DISTINCT FROM 'float32'
 OR before_image->'pixels' IS NOT DISTINCT FROM after_image->'pixels'
 OR before_image-'pixels' IS DISTINCT FROM after_image-'pixels' THEN RETURN false;END IF;
 has_alpha:=before_image->'alpha' IS NOT NULL AND before_image->'alpha'<>'null'::jsonb;
 total:=CASE WHEN has_alpha THEN 4 ELSE 2 END;
 IF jsonb_array_length(p_arrays)<>total THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_arrays) LOOP
  side:=CASE WHEN ordinal<total/2 THEN 'before' ELSE 'after' END;
  channel:=CASE WHEN has_alpha AND ordinal%2=1 THEN 'alpha' ELSE 'pixels' END;
  image:=CASE side WHEN 'before' THEN p_before ELSE p_after END;
  expected:=(image::jsonb)->channel;
  IF jsonb_typeof(expected) IS DISTINCT FROM 'object'
   OR jsonb_typeof(item) IS DISTINCT FROM 'object'
   OR NOT item ?& ARRAY['side','channel','ref','object_key','object_version',
    'sha256','bytes','media_type']
   OR (item-'side'-'channel'-'ref'-'object_key'-'object_version'-'sha256'-
    'bytes'-'media_type')<>'{}'::jsonb
   OR item->>'side' IS DISTINCT FROM side OR item->>'channel' IS DISTINCT FROM channel
   OR item->'ref' IS DISTINCT FROM expected
   OR item->>'sha256' IS DISTINCT FROM expected->>'sha256' THEN RETURN false;END IF;
  PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant
   AND id=(expected->>'artifact_id')::uuid FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
  locator:=photo.v28_restoration_preview_array(p_tenant,r.v28_restoration_preview_task_id,
   s.review_id,s.result_id,s.root_id,s.root_sha,side,channel,image,
   (expected->>'artifact_id')::uuid,expected->>'sha256',p_bucket);
  IF locator IS NULL OR item->>'object_key' IS DISTINCT FROM locator->>'key'
   OR item->>'object_version' IS DISTINCT FROM locator->>'version'
   OR item->'bytes' IS DISTINCT FROM locator->'bytes'
   OR item->>'media_type' IS DISTINCT FROM locator->>'media_type'
   OR item->>'sha256' IS DISTINCT FROM locator->>'sha256' THEN RETURN false;END IF;
  ordinal:=ordinal+1;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_preview_arrays_current(uuid,uuid,text,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_preview_arrays_current(uuid,uuid,text,text,text,jsonb)
 FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;

-- All four blocked tasks become ready atomically only after current-array proof.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_v28_restoration_preview(p_tenant uuid,p_run uuid,
 p_before text,p_after text,p_bucket text,p_arrays jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;d photo.backend_documents%ROWTYPE;s record;
 side text;variant text;image text;plan jsonb;params_sha text;task_id uuid;h bytea;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_run IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'preview' OR r.v28_restoration_preview_task_id IS NULL
 OR r.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.v28_restoration_preview_source(p_tenant,r.v28_restoration_preview_task_id);
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=s.run_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=r.v28_restoration_preview_task_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.reviews WHERE tenant_id=p_tenant AND id=s.review_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=s.root_id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=s.source_id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=s.candidate_id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF photo.v28_restoration_preview_arrays_current(p_tenant,p_run,p_before,p_after,p_bucket,p_arrays)
  IS DISTINCT FROM true THEN RETURN false;END IF;
 IF r.graph_sealed THEN RETURN r.status IN('running','completed');END IF;
 IF r.status<>'draft' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant
  AND q.holder_type='run' AND q.holder_id=p_run AND q.state='reserved'
  AND q.bytes=222298112 AND q.expires_at>clock_timestamp()) THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>4
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND (state<>'blocked' OR cancel_requested OR canonical_result_id IS NOT NULL))
 OR (SELECT count(*) FROM photo.backend_documents WHERE tenant_id=p_tenant
  AND run_id=p_run AND purpose='task_params' AND state='ready')<>4 THEN RETURN false;END IF;
 FOREACH side IN ARRAY ARRAY['before','after'] LOOP
  image:=CASE side WHEN 'before' THEN p_before ELSE p_after END;
  FOREACH variant IN ARRAY ARRAY['preview1600','preview2560'] LOOP
   h:=public.digest(uuid_send(p_run)||convert_to('restoration.'||side||':'||variant,'UTF8'),'sha1');
   h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
   task_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
   SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
    AND id=task_id AND node_key='restoration.'||side||':'||variant;
   IF NOT FOUND THEN RETURN false;END IF;
   SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
    AND owner_id=t.id AND artifact_id=t.params_artifact_id AND purpose='task_params'
    AND state='ready' FOR UPDATE;
   IF NOT FOUND THEN RETURN false;END IF;
   params_sha:=d.sha256;
   plan:=jsonb_build_object('id',t.id,'node_key',t.node_key,'operation',t.operation,
    'operation_version',t.operation_version,'resource_class',t.resource_class,
    'timeout_seconds',300,'params',jsonb_build_object('artifact_id',t.params_artifact_id,
     'sha256',params_sha),'inputs',t.inputs,'input_digest',t.input_digest,
    'execution_profile',t.execution_profile);
   IF photo.materialize_v28_restoration_preview_task(p_tenant,p_run,side,variant,image,plan)
    IS DISTINCT FROM true THEN RETURN false;END IF;
  END LOOP;
 END LOOP;
 UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1
  WHERE tenant_id=p_tenant AND id=p_run;
 UPDATE photo.tasks SET state='ready' WHERE tenant_id=p_tenant AND run_id=p_run
  AND state='blocked';
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_v28_restoration_preview(uuid,uuid,text,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v28_restoration_preview(uuid,uuid,text,text,text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.seal_v28_restoration_preview(uuid,uuid,text,text,text,jsonb)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
