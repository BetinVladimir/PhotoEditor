-- +goose Up
-- Read authority only. Contracts/source semantics require independent validators.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_export_document(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,
 p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;frozen jsonb;request jsonb;scope jsonb;rid uuid;project uuid;sid uuid;doc_purpose text;body text;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_contract IS NULL
 OR p_contract NOT IN('Params','InterchangePlan','InputSnapshot','WorkingImage','FrozenRenderRecipe','EditingRecipeV3','StyleV2',
 'CorrectionPlan','Override','LayerStack','MaskRef','CandidateProfile','DescriptorShard','StyleBackgroundAsset','EncodedImage','WorkingPixels','WorkingAlpha','PixelArray','MaskPixels','MaskArray','DenseWarpField','DenseWarpArray') THEN RETURN;END IF;
 SELECT i.response_body INTO saved FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant AND i.principal_type='user'
 AND i.principal_id=p_actor AND i.route='Interchange.Export.Pending' AND i.key=p_key AND i.request_hash=p_request_sha
 AND i.response_status IS NULL AND i.expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL OR saved->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text OR saved->>'actor_id' IS DISTINCT FROM p_actor::text THEN RETURN;END IF;
 frozen:=saved->'frozen';request:=frozen->'request';project:=(request->>'project_id')::uuid;sid:=(request->>'snapshot_id')::uuid;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);
 -- Reuse the exact frozen request/identity/draft/ACL guard for every lookup.
 scope:=photo.v30_interchange_export_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,
 (frozen->'params'->>'artifact_id')::uuid,'task_params',frozen->>'params_jcs');
 IF scope IS NULL THEN RETURN;END IF;
 IF p_contract IN('Params','InterchangePlan') AND p_artifact IN((frozen->'params'->>'artifact_id')::uuid,(frozen->'plan'->>'artifact_id')::uuid) THEN
  doc_purpose:=CASE WHEN p_contract='Params' THEN 'task_params' ELSE 'v30_interchange_export_plan' END;
  body:=CASE WHEN p_contract='Params' THEN frozen->>'params_jcs' ELSE frozen->>'plan_jcs' END;
  IF photo.v30_interchange_export_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,p_artifact,doc_purpose,body) IS NULL THEN RETURN;END IF;
  RETURN QUERY SELECT a.bucket,a.key,a.object_version,a.sha256,a.bytes,a.media_type
   FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
   WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=(scope->>'owner')::uuid AND b.artifact_id=p_artifact
   AND b.purpose=doc_purpose AND b.state='ready' AND b.sha256=p_sha AND b.bytes=octet_length(body)
   AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
   AND a.key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||p_artifact AND a.output_role=doc_purpose
   AND a.media_type='application/json' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
  RETURN;
 END IF;
 -- Project linkage authorizes reading immutable bytes only. It does not prove
 -- a requested contract or that encoded content came from this snapshot item.
 RETURN QUERY SELECT a.bucket,a.key,a.object_version,a.sha256,a.bytes,a.media_type FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.state='ready' AND a.bytes BETWEEN 1 AND 137438953472
 AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=p_tenant AND l.project_id=project AND l.artifact_id=a.id AND l.sha256=a.sha256)
 OR (p_contract='InputSnapshot' AND frozen->'snapshot'=jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha)
 AND EXISTS(SELECT 1 FROM photo.snapshots s WHERE s.tenant_id=p_tenant AND s.id=sid AND s.artifact_id=a.id))) FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
