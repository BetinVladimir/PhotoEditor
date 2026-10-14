-- +goose Up
-- Read only after repeating current lease, source and encode producer authority.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_delivery_metadata_proof(p_tenant uuid,p_expansion uuid,p_token uuid)
RETURNS TABLE(proof_jcs text,proof_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body text;pin text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_token IS NULL THEN RETURN;END IF;
 SELECT e.v30_metadata_proof_jcs,e.v30_metadata_proof_sha INTO body,pin FROM photo.graph_expansions e
 WHERE e.tenant_id=p_tenant AND e.id=p_expansion AND e.kind='v30.delivery.metadata/1';
 IF body IS NULL OR pin IS NULL THEN RETURN;END IF;
 IF photo.pin_v30_delivery_metadata_proof(p_tenant,p_expansion,p_token,body,pin) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT body,pin;
END $$;
ALTER FUNCTION photo.read_v30_delivery_metadata_proof(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_delivery_metadata_proof(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_delivery_metadata_proof(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
