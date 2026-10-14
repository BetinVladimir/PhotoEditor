-- +goose Up
-- Parent-lease wrappers around the established fenced V2 transport document
-- lifecycle. Only the one signer chunk and manifest are admitted here.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v31_delivery_signer_transport(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_source jsonb,p_lineage_bytes bigint,p_params text,p_bucket text,
 p_task jsonb,p_purpose text,p_sha text,p_bytes bigint)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;tenant uuid;run_id uuid;task_id uuid;
 result record;
BEGIN
 IF p_purpose IS NULL OR p_purpose NOT IN('v2_input_chunk_0','v2_input_manifest')
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576 THEN
  RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 scope:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF scope IS NULL OR photo.v31_delivery_signer_transport_ready(
  p_expansion,p_token,p_ordinal,p_preset,p_source,p_lineage_bytes,
  p_params,p_bucket,p_task) IS DISTINCT FROM true THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 tenant:=(scope->>'tenant_id')::uuid;
 run_id:=photo.v30_uuid5((scope->>'workflow_id')::uuid,'run');
 task_id:=(scope->>'task_id')::uuid;
 SELECT x.outcome,x.artifact_id,x.token,x.ready INTO result
  FROM photo.reserve_v2_input_document(tenant,run_id,task_id,p_purpose,
   p_sha,p_bytes,p_bucket) x;
 IF NOT FOUND THEN RAISE EXCEPTION 'SIGNER_TRANSPORT_RESERVATION'
  USING ERRCODE='23514';END IF;
 IF photo.v30_delivery_signer_delegation_scope(p_expansion,p_token,
  p_ordinal,p_preset) IS DISTINCT FROM scope OR
  photo.v31_delivery_signer_transport_ready(p_expansion,p_token,p_ordinal,
   p_preset,p_source,p_lineage_bytes,p_params,p_bucket,p_task)
   IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'SIGNER_TRANSPORT_STALE' USING ERRCODE='23514';END IF;
 RETURN QUERY SELECT result.outcome,result.artifact_id,result.token,result.ready;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v31_delivery_signer_transport(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v31_delivery_signer_transport(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.reserve_v31_delivery_signer_transport(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb,text,text,bigint)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v31_delivery_signer_transport(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_source jsonb,p_lineage_bytes bigint,p_params text,p_bucket text,
 p_task jsonb,p_purpose text,p_artifact uuid,p_document_token uuid,
 p_sha text,p_bytes bigint,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;tenant uuid;run_id uuid;task_id uuid;accepted boolean;
BEGIN
 IF p_purpose IS NULL OR p_purpose NOT IN('v2_input_chunk_0','v2_input_manifest')
  OR p_artifact IS NULL OR p_document_token IS NULL
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576
  OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024
  THEN RETURN false;END IF;
 scope:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF scope IS NULL OR photo.v31_delivery_signer_transport_ready(
  p_expansion,p_token,p_ordinal,p_preset,p_source,p_lineage_bytes,
  p_params,p_bucket,p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 tenant:=(scope->>'tenant_id')::uuid;
 run_id:=photo.v30_uuid5((scope->>'workflow_id')::uuid,'run');
 task_id:=(scope->>'task_id')::uuid;
 accepted:=photo.complete_v2_input_document(tenant,run_id,task_id,
  p_purpose,p_artifact,p_document_token,p_sha,p_bytes,p_bucket,p_version);
 IF accepted IS DISTINCT FROM true THEN RETURN false;END IF;
 IF photo.v30_delivery_signer_delegation_scope(p_expansion,p_token,
  p_ordinal,p_preset) IS DISTINCT FROM scope OR
  photo.v31_delivery_signer_transport_ready(p_expansion,p_token,p_ordinal,
   p_preset,p_source,p_lineage_bytes,p_params,p_bucket,p_task)
   IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'SIGNER_TRANSPORT_STALE' USING ERRCODE='23514';END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v31_delivery_signer_transport(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb,text,uuid,uuid,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v31_delivery_signer_transport(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb,text,uuid,uuid,text,bigint,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.complete_v31_delivery_signer_transport(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb,text,uuid,uuid,text,bigint,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
