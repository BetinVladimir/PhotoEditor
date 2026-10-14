-- +goose Up
-- RFC 4122 UUIDv5 identity shared with Go uuid.NewSHA1.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_uuid_v5(p_namespace uuid,p_name text) RETURNS uuid
LANGUAGE plpgsql IMMUTABLE STRICT SET search_path=pg_catalog,photo AS $$
DECLARE value bytea;
BEGIN
 value:=substring(public.digest(uuid_send(p_namespace)||convert_to(p_name,'UTF8'),'sha1') FROM 1 FOR 16);
 value:=set_byte(value,6,(get_byte(value,6)&15)|80);
 value:=set_byte(value,8,(get_byte(value,8)&63)|128);
 RETURN encode(value,'hex')::uuid;
END $$;
ALTER FUNCTION photo.semantic_uuid_v5(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_uuid_v5(uuid,text) FROM PUBLIC;
CREATE FUNCTION photo.semantic_approved_child_params_ready(p_tenant uuid,p_parent uuid,p_intent uuid,p_child uuid,p_decisions jsonb,p_params jsonb,p_documents jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_child IS NULL OR p_child='00000000-0000-0000-0000-000000000000'::uuid
 OR p_child=p_parent OR NOT photo.semantic_approved_params_ready(p_tenant,p_parent,p_intent,p_decisions,p_params,p_documents) THEN RETURN false;END IF;
 RETURN NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_params) p
 WHERE (p->>'task_id')::uuid IS DISTINCT FROM photo.semantic_uuid_v5(p_child,'semantic.edit:'||(p->>'target_id')::uuid::text)
 OR (p->>'artifact_id')::uuid IS DISTINCT FROM photo.semantic_uuid_v5((p->>'task_id')::uuid,'Params'));
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_approved_child_params_ready(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_child_params_ready(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
