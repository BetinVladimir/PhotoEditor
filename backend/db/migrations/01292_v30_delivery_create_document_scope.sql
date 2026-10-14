-- +goose Up
-- Private exact-document authority for future fenced reservation/completion.
-- This predicate alone cannot reserve storage, create a run or dispatch work.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_create_document_scope(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_artifact uuid,p_purpose text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;document jsonb;recipes jsonb;selection jsonb;project uuid;snapshot uuid;rid uuid;owner uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$'
 OR p_key IS NULL OR octet_length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$'
 OR p_purpose IS NULL OR p_purpose NOT IN('v30_delivery_snapshot','v30_delivery_preset','v30_delivery_policy','v30_delivery_input','task_params') THEN RETURN NULL;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='POST /delivery-packages' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL OR saved->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text OR saved->>'actor_id' IS DISTINCT FROM p_actor::text
 OR saved->>'request_sha' IS DISTINCT FROM p_request_sha OR jsonb_typeof(saved->'documents') IS DISTINCT FROM 'array'
 OR jsonb_array_length(saved->'documents') NOT BETWEEN 5 AND 20 THEN RETURN NULL;END IF;
 -- The exact immutable document body and ID were saved before external IO.
 SELECT value INTO document FROM jsonb_array_elements(saved->'documents') WHERE value->'ref'->>'artifact_id'=p_artifact::text;
 IF document IS NULL OR (SELECT count(*) FROM jsonb_array_elements(saved->'documents') WHERE value->'ref'->>'artifact_id'=p_artifact::text)<>1
 OR document->>'purpose' IS DISTINCT FROM p_purpose OR document->>'body_jcs' IS DISTINCT FROM p_body
 OR document->'ref'->>'sha256' IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex') THEN RETURN NULL;END IF;
 project:=(saved->>'project_id')::uuid;
 snapshot:=(saved->'catalog'->>'Snapshot')::uuid;
 IF project IS NULL OR photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT jsonb_agg(value->'recipe' ORDER BY ordinality) INTO recipes FROM jsonb_array_elements(saved->'items') WITH ORDINALITY;
 selection:=photo.resolve_v30_delivery_selection(p_tenant,project,snapshot,p_actor,p_session,recipes);
 IF selection IS NULL OR selection->'items' IS DISTINCT FROM saved->'items' THEN RETURN NULL;END IF;
 rid:=photo.v30_uuid5(p_tenant,'delivery:create:'||p_actor||':'||p_key);
 owner:=CASE WHEN p_purpose='task_params' THEN photo.v30_uuid5(photo.v30_uuid5(rid,'workflow'),'v2.delivery.prepare:0') ELSE p_artifact END;
 RETURN jsonb_build_object('tenant',p_tenant,'run',rid,'owner',owner,'project',project,'snapshot',snapshot,
 'artifact',p_artifact,'purpose',p_purpose,'sha',encode(sha256(convert_to(p_body,'UTF8')),'hex'),'bytes',octet_length(p_body));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_create_document_scope(uuid,uuid,uuid,text,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_create_document_scope(uuid,uuid,uuid,text,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
