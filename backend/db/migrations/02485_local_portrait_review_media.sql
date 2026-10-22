-- +goose Up
-- +goose StatementBegin
-- Local portrait has one ImageResult root. Never infer its candidate from an
-- arbitrary artifact of the same task; use the committed exact adoption receipt.
CREATE FUNCTION photo.v28_local_portrait_derivative_source(p_tenant uuid,p_task uuid)
RETURNS TABLE(project_id uuid,run_id uuid,result_id uuid,root_id uuid,root_sha text,attempt integer,fence bigint)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT w.project_id,r.id,z.id,root.id,root.sha256,t.attempt,t.fencing_token
 FROM photo.tasks t
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id AND p.archived_at IS NULL
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 CROSS JOIN LATERAL photo.v28_local_portrait_adoption_params(t.tenant_id,t.id,t.attempt,t.fencing_token,z.id) original
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id AND root.id::text=r.v28_local_result_receipt->'root'->>'artifact_id'
 JOIN photo.artifacts candidate ON candidate.tenant_id=t.tenant_id AND candidate.id::text=r.v28_local_result_receipt->'candidate'->>'artifact_id'
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation IN('portrait.hair','portrait.clothing')
 AND r.v28_local_result_receipt->>'task_id'=t.id::text
 AND r.v28_local_result_receipt->>'result_id'=z.id::text
 AND r.v28_local_result_receipt->>'attempt'=t.attempt::text
 AND r.v28_local_result_receipt->>'fence'=t.fencing_token::text
 AND r.v28_local_result_receipt->'base'=original.params_jcs::jsonb->'base'->'source'
 AND root.sha256=r.v28_local_result_receipt->'root'->>'sha256'
 AND candidate.sha256=r.v28_local_result_receipt->'candidate'->>'sha256'
 AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
 AND root.output_role='document' AND candidate.output_role='working_image'
 AND root.media_type='application/json' AND candidate.media_type='application/json'
 AND root.state='ready' AND candidate.state='ready'
 AND root.verified_at IS NOT NULL AND candidate.verified_at IS NOT NULL
 AND root.created_by_task_id=t.id AND candidate.created_by_task_id=t.id
 AND root.produced_attempt=t.attempt AND candidate.produced_attempt=t.attempt
 AND root.produced_fence=t.fencing_token AND candidate.produced_fence=t.fencing_token
 AND root.object_version NOT IN('','null') AND candidate.object_version NOT IN('','null')
 AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
 AND (candidate.expires_at IS NULL OR candidate.expires_at>clock_timestamp())
 AND EXISTS(SELECT FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
  AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 AND EXISTS(SELECT FROM photo.v2_artifact_links l WHERE l.tenant_id=t.tenant_id AND l.workflow_id=w.id
  AND l.project_id=w.project_id AND l.artifact_id=root.id AND l.sha256=root.sha256
  AND l.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex'))
 AND EXISTS(SELECT FROM photo.v2_artifact_links l WHERE l.tenant_id=t.tenant_id AND l.workflow_id=w.id
  AND l.project_id=w.project_id AND l.artifact_id=candidate.id AND l.sha256=candidate.sha256
  AND l.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex'));
$$;
ALTER FUNCTION photo.v28_local_portrait_derivative_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_portrait_derivative_source(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
-- Only the guarded shared derivative coordinator calls this private helper.
ALTER FUNCTION photo.v26_render_derivative_source(uuid,uuid) RENAME TO v26_render_derivative_source_before_local_portrait;
REVOKE ALL ON FUNCTION photo.v26_render_derivative_source_before_local_portrait(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.v26_render_derivative_source(p_tenant uuid,p_task uuid)
RETURNS TABLE(project_id uuid,run_id uuid,result_id uuid,root_id uuid,root_sha text,attempt integer,fence bigint)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT * FROM photo.v26_render_derivative_source_before_local_portrait(p_tenant,p_task)
 UNION ALL SELECT * FROM photo.v28_local_portrait_derivative_source(p_tenant,p_task);
$$;
ALTER FUNCTION photo.v26_render_derivative_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_derivative_source(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_derivative_source(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;BEGIN
 definition:=pg_get_functiondef('photo.enqueue_next_v26_render_derivative(text)'::regprocedure);
 needle:='t.operation IN (''render.compose'',''optical.negative'',''portrait.open_eyes'',''image.relight'')';
 IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'LOCAL_PORTRAIT_DERIVATIVE_POLL_DRIFT';END IF;
 EXECUTE replace(definition,needle,'t.operation IN (''render.compose'',''optical.negative'',''portrait.open_eyes'',''image.relight'',''portrait.hair'',''portrait.clothing'')');
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_portrait_base_preview_ready(p_tenant uuid,p_review uuid,p_srgb text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.reviews review
  JOIN photo.runs donor ON donor.tenant_id=review.tenant_id AND donor.id=review.run_id
  JOIN photo.v2_workflows workflow ON workflow.tenant_id=donor.tenant_id AND workflow.run_id=donor.id
  JOIN photo.asset_versions version ON version.tenant_id=review.tenant_id
   AND version.id=((donor.config_snapshot->'v28_local_portrait'->'intent'->>'params_jcs')::jsonb->'base'->>'asset_version_id')::uuid
   AND version.master_artifact_id=review.base_artifact_id
  JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
  JOIN photo.artifacts base ON base.tenant_id=review.tenant_id AND base.id=review.base_artifact_id
   AND base.sha256=version.content_hash
  JOIN photo.derivatives derivative ON derivative.tenant_id=version.tenant_id
   AND derivative.asset_version_id=version.id AND derivative.variant='preview1600'
   AND derivative.profile_hash=encode(sha256(convert_to(format(
    '{"icc_sha256":"%s","long_edge":1600,"metadata_policy":"stripped","no_upscale":true,"quality":88,"schema_version":1,"variant":"preview1600"}',p_srgb),'UTF8')),'hex')
  JOIN photo.artifacts image ON image.tenant_id=derivative.tenant_id
   AND image.id=derivative.artifact_id
  JOIN photo.tasks task ON task.tenant_id=derivative.tenant_id AND task.id=derivative.task_id
  JOIN photo.runs presentation ON presentation.tenant_id=task.tenant_id AND presentation.id=task.run_id
  JOIN photo.task_results result ON result.tenant_id=task.tenant_id
   AND result.task_id=task.id AND result.id=task.canonical_result_id
  JOIN photo.artifacts manifest ON manifest.tenant_id=task.tenant_id
   AND manifest.created_by_task_id=task.id AND manifest.output_role='derivative_manifest'
  JOIN photo.output_uploads image_upload ON image_upload.tenant_id=image.tenant_id
   AND image_upload.task_id=task.id AND image_upload.artifact_id=image.id
  JOIN photo.output_uploads manifest_upload ON manifest_upload.tenant_id=manifest.tenant_id
   AND manifest_upload.task_id=task.id AND manifest_upload.artifact_id=manifest.id
  WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
   AND p_srgb ~ '^[0-9a-f]{64}$' AND review.tenant_id=p_tenant AND review.id=p_review
   AND review.kind IN('portrait.hair','portrait.clothing') AND workflow.project_id=asset.project_id
   AND asset.deleted_at IS NULL AND asset.upload_state='verified'
   AND version.review_state='accepted' AND derivative.status='ready'
   AND task.operation='media.preview' AND task.operation_version='1'
   AND task.state='succeeded' AND NOT task.cancel_requested
   AND presentation.status='completed' AND NOT presentation.cancel_requested
   AND result.attempt=task.attempt AND result.fencing_token=task.fencing_token
   AND task.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',base.id::text,'sha256',base.sha256))
   AND jsonb_array_length(result.outputs)=2
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',image.id::text,'sha256',image.sha256),
    jsonb_build_object('artifact_id',manifest.id::text,'sha256',manifest.sha256))
   AND base.state='ready' AND image.state='ready' AND manifest.state='ready'
   AND base.verified_at IS NOT NULL AND image.verified_at IS NOT NULL AND manifest.verified_at IS NOT NULL
   AND image.output_role='derivative' AND image.media_type IN('image/jpeg','image/webp')
   AND manifest.media_type='application/json'
   AND image.created_by_task_id=task.id
   AND image.produced_attempt=task.attempt AND manifest.produced_attempt=task.attempt
   AND image.produced_fence=task.fencing_token AND manifest.produced_fence=task.fencing_token
   AND image_upload.state='ready' AND manifest_upload.state='ready'
   AND image_upload.attempt=task.attempt AND manifest_upload.attempt=task.attempt
   AND image_upload.fence=task.fencing_token AND manifest_upload.fence=task.fencing_token
   AND image_upload.object_version=image.object_version
   AND manifest_upload.object_version=manifest.object_version
   AND image_upload.bucket=image.bucket AND image_upload.object_key=image.object_key
   AND image_upload.sha256=image.sha256 AND image_upload.bytes=image.bytes
   AND image_upload.media_type=image.media_type AND image_upload.role=image.output_role
   AND manifest_upload.bucket=manifest.bucket AND manifest_upload.object_key=manifest.object_key
   AND manifest_upload.sha256=manifest.sha256 AND manifest_upload.bytes=manifest.bytes
   AND manifest_upload.media_type=manifest.media_type AND manifest_upload.role=manifest.output_role
   AND base.object_version NOT IN('','null')
   AND image.object_version NOT IN('','null') AND manifest.object_version NOT IN('','null')
   AND (base.expires_at IS NULL OR base.expires_at>clock_timestamp())
   AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp())
   AND (manifest.expires_at IS NULL OR manifest.expires_at>clock_timestamp()));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_local_portrait_base_preview_ready(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_portrait_base_preview_ready(uuid,uuid,text) FROM PUBLIC;

-- A donor decision requires both independently adopted after derivatives.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_portrait_review_preview_ready(p_tenant uuid,p_review uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.reviews v
  JOIN photo.tasks origin ON origin.tenant_id=v.tenant_id AND origin.id=v.task_id
  CROSS JOIN LATERAL photo.v28_local_portrait_derivative_source(p_tenant,origin.id) source
  JOIN photo.runs preview ON preview.tenant_id=v.tenant_id
   AND preview.v26_derivative_task_id=origin.id
   AND preview.config_snapshot->>'source_result_id'=source.result_id::text
   AND preview.config_snapshot->'root'=jsonb_build_object('artifact_id',source.root_id,'sha256',source.root_sha)
  WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
   AND v.tenant_id=p_tenant AND v.id=p_review AND v.kind IN('portrait.hair','portrait.clothing')
   AND v.state IN('pending','decided') AND v.run_id=source.run_id
   AND preview.kind='preview' AND preview.status='completed'
   AND preview.graph_sealed AND NOT preview.cancel_requested
   AND preview.config_snapshot->>'source_attempt'=source.attempt::text
   AND preview.config_snapshot->>'source_fence'=source.fence::text
   AND photo.v28_local_portrait_base_preview_ready(p_tenant,p_review,
    preview.config_snapshot->>'srgb_sha256')
   AND (SELECT count(DISTINCT t.node_key) FROM photo.tasks t
    JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.owner_id=t.id
     AND d.run_id=t.run_id AND d.artifact_id=t.params_artifact_id
     AND d.purpose='task_params' AND d.state='ready'
    JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.task_id=t.id
     AND result.id=t.canonical_result_id AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token
    CROSS JOIN LATERAL (SELECT photo.v26_derivative_receipt_document(d.v26_derivative_receipt) AS body) receipt
    CROSS JOIN LATERAL jsonb_array_elements(receipt.body->'objects') mo
    CROSS JOIN LATERAL jsonb_array_elements(receipt.body->'objects') io
    JOIN photo.artifacts manifest ON manifest.tenant_id=t.tenant_id
     AND manifest.id::text=mo->>'artifact_id' AND manifest.sha256=mo->>'sha256'
     AND manifest.id::text=receipt.body->>'manifest_id'
    JOIN photo.artifacts image ON image.tenant_id=t.tenant_id
     AND image.id::text=io->>'artifact_id' AND image.sha256=io->>'sha256'
     AND image.id::text=receipt.body->'manifest'->'derivative'->>'artifact_id'
     AND image.sha256=receipt.body->'manifest'->'derivative'->>'sha256'
    JOIN photo.output_uploads mu ON mu.tenant_id=t.tenant_id AND mu.task_id=t.id AND mu.artifact_id=manifest.id
    JOIN photo.output_uploads iu ON iu.tenant_id=t.tenant_id AND iu.task_id=t.id AND iu.artifact_id=image.id
    WHERE t.tenant_id=p_tenant AND t.run_id=preview.id
     AND t.operation='media.preview' AND t.operation_version='1'
     AND t.state='succeeded' AND NOT t.cancel_requested
     AND t.node_key IN('render.derivative:preview1600','render.derivative:preview2560')
     AND receipt.body->'manifest'->'source'=
      jsonb_build_object('artifact_id',v.candidate_artifact_id,'sha256',v.candidate_hash)
     AND d.v26_derivative_receipt->>'result_id'=t.canonical_result_id::text
     AND d.v26_derivative_receipt->>'attempt'=t.attempt::text
     AND d.v26_derivative_receipt->>'fence'=t.fencing_token::text
     AND receipt.body->'manifest'->>'variant'=split_part(t.node_key,':',2)
     AND receipt.body->'manifest'->>'profile_hash'=encode(sha256(convert_to(format(
      '{"icc_sha256":"%s","long_edge":%s,"metadata_policy":"stripped","no_upscale":true,"quality":88,"schema_version":1,"variant":"%s"}',
      preview.config_snapshot->>'srgb_sha256',CASE WHEN t.node_key='render.derivative:preview1600' THEN 1600 ELSE 2560 END,
      split_part(t.node_key,':',2)),'UTF8')),'hex')
     AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',manifest.id,'sha256',manifest.sha256),
      jsonb_build_object('artifact_id',image.id,'sha256',image.sha256))
     AND manifest.output_role='derivative_manifest' AND manifest.media_type='application/json'
     AND image.output_role='derivative' AND image.media_type IN('image/jpeg','image/webp')
     AND manifest.bytes BETWEEN 2 AND 1048576 AND image.bytes BETWEEN 2 AND 54525952
     AND manifest.state='ready' AND image.state='ready'
     AND manifest.verified_at IS NOT NULL AND image.verified_at IS NOT NULL
     AND manifest.created_by_task_id=t.id AND image.created_by_task_id=t.id
     AND manifest.produced_attempt=t.attempt AND image.produced_attempt=t.attempt
     AND manifest.produced_fence=t.fencing_token AND image.produced_fence=t.fencing_token
     AND manifest.bucket=mo->>'bucket' AND manifest.object_key=mo->>'key'
     AND manifest.object_version=mo->>'version' AND manifest.bytes=(mo->>'bytes')::bigint AND manifest.media_type=mo->>'media_type'
     AND image.bucket=io->>'bucket' AND image.object_key=io->>'key'
     AND image.object_version=io->>'version' AND image.bytes=(io->>'bytes')::bigint AND image.media_type=io->>'media_type'
     AND mu.state='ready' AND iu.state='ready'
     AND mu.attempt=t.attempt AND iu.attempt=t.attempt AND mu.fence=t.fencing_token AND iu.fence=t.fencing_token
     AND mu.bucket=manifest.bucket AND mu.object_key=manifest.object_key AND mu.object_version=manifest.object_version
     AND mu.sha256=manifest.sha256 AND mu.bytes=manifest.bytes AND mu.media_type=manifest.media_type AND mu.role=manifest.output_role
     AND iu.bucket=image.bucket AND iu.object_key=image.object_key AND iu.object_version=image.object_version
     AND iu.sha256=image.sha256 AND iu.bytes=image.bytes AND iu.media_type=image.media_type AND iu.role=image.output_role
     AND manifest.object_version NOT IN('','null') AND image.object_version NOT IN('','null')
     AND (manifest.expires_at IS NULL OR manifest.expires_at>clock_timestamp())
     AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp()))=2
 );
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_local_portrait_review_preview_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_portrait_review_preview_ready(uuid,uuid) FROM PUBLIC;

-- +goose StatementBegin
DO $$ DECLARE definition text;signature text;needle text;replacement text;BEGIN
 needle:='v.kind IN (''render.compose'',''optical.negative'',''portrait.open_eyes'',''image.relight'')';
 replacement:='v.kind IN (''render.compose'',''optical.negative'',''portrait.open_eyes'',''image.relight'',''portrait.hair'',''portrait.clothing'')'
  ||' AND (v.kind NOT IN(''portrait.hair'',''portrait.clothing'') OR ('
  ||'version.id::text=((parent.config_snapshot->''v28_local_portrait''->''intent''->>''params_jcs'')::jsonb)->''base''->>''asset_version_id'''
  ||' AND version.master_artifact_id::text=((parent.config_snapshot->''v28_local_portrait''->''intent''->>''params_jcs'')::jsonb)->''base''->''source''->>''artifact_id'''
  ||' AND version.content_hash=((parent.config_snapshot->''v28_local_portrait''->''intent''->>''params_jcs'')::jsonb)->''base''->''source''->>''sha256''))';
 FOREACH signature IN ARRAY ARRAY['photo.v26_review_media_source_ready(uuid)','photo.enqueue_v26_review_before_media(uuid,uuid)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'LOCAL_PORTRAIT_BEFORE_MEDIA_KIND_DRIFT';END IF;
  EXECUTE replace(definition,needle,replacement);
 END LOOP;
END $$;
CREATE FUNCTION photo.enqueue_next_v28_local_portrait_before_media()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected record;event_id uuid;
BEGIN
 SELECT v.tenant_id,v.id INTO selected FROM photo.reviews v
 JOIN photo.tenants tenant ON tenant.id=v.tenant_id AND tenant.status='active'
 JOIN photo.runs r ON r.tenant_id=v.tenant_id AND r.id=v.run_id
 JOIN photo.tasks t ON t.tenant_id=v.tenant_id AND t.id=v.task_id AND t.run_id=r.id
 CROSS JOIN LATERAL photo.v28_local_portrait_derivative_source(t.tenant_id,t.id) scoped
 JOIN photo.asset_versions version ON version.tenant_id=v.tenant_id
  AND version.id::text=((r.config_snapshot->'v28_local_portrait'->'intent'->>'params_jcs')::jsonb)->'base'->>'asset_version_id'
  AND version.master_artifact_id=v.base_artifact_id
 WHERE v.kind IN('portrait.hair','portrait.clothing') AND v.state='pending'
 AND r.status='waiting_review' AND r.v28_local_result_receipt->>'review_id'=v.id::text
 AND version.created_by_run_id IS NOT NULL
 AND NOT EXISTS(SELECT FROM photo.media_retry_jobs j WHERE j.tenant_id=v.tenant_id
  AND j.review_id=v.id AND j.asset_version_id=version.id AND j.origin='review')
 ORDER BY v.created_at,v.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 event_id:=photo.enqueue_v26_review_before_media(selected.tenant_id,selected.id);
 RETURN event_id IS NOT NULL;
END $$;
ALTER FUNCTION photo.enqueue_next_v28_local_portrait_before_media() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_next_v28_local_portrait_before_media() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.enqueue_next_v28_local_portrait_before_media() TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;BEGIN
 definition:=pg_get_functiondef('photo.v28_local_portrait_review_decision_ready(uuid,uuid,uuid,uuid)'::regprocedure);
 needle:='RETURN q->>''review_id''=v.id::text';
 IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'LOCAL_PORTRAIT_DECISION_MEDIA_DRIFT';END IF;
 EXECUTE replace(definition,needle,'RETURN photo.v28_local_portrait_review_preview_ready(p_tenant,p_review) AND q->>''review_id''=v.id::text');
 definition:=pg_get_functiondef('photo.get_v28_local_portrait_review_selection(uuid,uuid,uuid,uuid)'::regprocedure);
 needle:='AND photo.v28_local_portrait_result_ready(p_tenant,r.id,p_actor,p_session)';
 IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'LOCAL_PORTRAIT_SELECTION_MEDIA_DRIFT';END IF;
 EXECUTE replace(definition,needle,needle||' AND photo.v28_local_portrait_review_preview_ready(p_tenant,p_review)');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
