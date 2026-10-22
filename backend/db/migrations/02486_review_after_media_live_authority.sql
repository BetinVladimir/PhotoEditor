-- +goose Up
-- Preserve all existing principal, consent, qualification and source gates.
-- +goose StatementBegin
CREATE FUNCTION photo.review_after_media_live(p_tenant uuid,p_preview uuid,p_source uuid,p_sha text,p_srgb text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND p_srgb ~ '^[0-9a-f]{64}$' AND p_sha ~ '^[0-9a-f]{64}$'
 AND EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_preview
  AND r.kind='preview' AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested)
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
    WHERE t.tenant_id=p_tenant AND t.run_id=p_preview
     AND t.operation='media.preview' AND t.operation_version='1'
     AND t.state='succeeded' AND NOT t.cancel_requested
     AND t.node_key IN('render.derivative:preview1600','render.derivative:preview2560')
     AND receipt.body->'manifest'->'source'=
      jsonb_build_object('artifact_id',p_source,'sha256',p_sha)
     AND d.v26_derivative_receipt->>'result_id'=t.canonical_result_id::text
     AND d.v26_derivative_receipt->>'attempt'=t.attempt::text
     AND d.v26_derivative_receipt->>'fence'=t.fencing_token::text
     AND receipt.body->'manifest'->>'variant'=split_part(t.node_key,':',2)
     AND receipt.body->'manifest'->>'profile_hash'=encode(sha256(convert_to(format(
      '{"icc_sha256":"%s","long_edge":%s,"metadata_policy":"stripped","no_upscale":true,"quality":88,"schema_version":1,"variant":"%s"}',
      p_srgb,CASE WHEN t.node_key='render.derivative:preview1600' THEN 1600 ELSE 2560 END,
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
     AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp()))=2,false);
$$;
ALTER FUNCTION photo.review_after_media_live(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.review_after_media_live(uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_portal,photo_gateway,photo_maintenance;
DO $$ DECLARE signature text;definition text;needle text;replacement text;BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.v28_open_eyes_review_preview_ready(uuid,uuid)',
  'photo.v28_relight_review_preview_ready(uuid,uuid)',
  'photo.get_v28_open_eyes_review_selection(uuid,uuid,uuid,uuid)',
  'photo.get_v28_relight_review_selection_pre_authority(uuid,uuid,uuid,uuid)'
 ] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  needle:='AND preview.kind=''preview''';
  IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'REVIEW_AFTER_MEDIA_LIVE_ANCHOR: %',signature;END IF;
  replacement:=needle||' AND photo.review_after_media_live(p_tenant,preview.id,v.candidate_artifact_id,v.candidate_hash,preview.config_snapshot->>''srgb_sha256'')';
  EXECUTE replace(definition,needle,replacement);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
