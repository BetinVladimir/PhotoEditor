-- +goose Up
-- A restarted scheduler may recover only the exact physically ready envelope
-- under the current parent expansion and renewed V31 signing authority.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_delivery_signed_envelope(p_expansion uuid,p_token uuid,
 p_artifact uuid,p_sha text)
RETURNS TABLE(envelope_jcs text,envelope_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE x photo.graph_expansions%ROWTYPE;d photo.backend_documents%ROWTYPE;scope jsonb;owner uuid;
BEGIN
 IF p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1'
  AND state='open' AND expansion_token=p_token AND expansion_lease_until>clock_timestamp();
 IF NOT FOUND THEN RETURN;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5(x.run_id,'workflow'),'v2.delivery.assemble:0');
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=x.tenant_id
  AND b.run_id=x.run_id AND b.owner_id=owner AND b.purpose='v30_delivery_signed_envelope'
  AND b.artifact_id=p_artifact AND b.sha256=p_sha AND b.state='ready'
  AND b.v30_delivery_document_jcs IS NOT NULL;
 IF NOT FOUND THEN RETURN;END IF;
 scope:=photo.v30_delivery_signed_document_scope(p_expansion,p_token,d.purpose,d.v30_delivery_document_jcs);
 IF scope IS NULL OR scope->>'tenant' IS DISTINCT FROM x.tenant_id::text
  OR scope->>'run' IS DISTINCT FROM x.run_id::text OR scope->>'owner' IS DISTINCT FROM owner::text
  OR scope->>'sha' IS DISTINCT FROM p_sha OR (scope->>'bytes')::bigint IS DISTINCT FROM d.bytes
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND a.id=d.artifact_id
   AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
   AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
   AND a.object_key='tenants/'||x.tenant_id||'/runs/'||x.run_id||'/documents/'||d.artifact_id
   AND a.media_type='application/json' AND a.output_role=d.purpose AND a.verified_at IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN;END IF;
 RETURN QUERY SELECT d.v30_delivery_document_jcs,d.sha256;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v30_delivery_signed_envelope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_delivery_signed_envelope(uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.read_v30_delivery_signed_envelope(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
