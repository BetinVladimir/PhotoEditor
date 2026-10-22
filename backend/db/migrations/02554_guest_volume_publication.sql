-- +goose Up
-- Upload-only sessions may remain valid on a draft collection. A guest archive
-- bearer, including one created before unpublication, must not inherit that.
-- +goose StatementBegin
CREATE FUNCTION photo.guest_volume_collection_current(p_tenant uuid,p_collection uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 PERFORM 1 FROM photo.collections c WHERE c.tenant_id=p_tenant AND c.id=p_collection
  AND c.state='published' AND c.deleted_at IS NULL FOR SHARE;
 RETURN FOUND;
END $$;
ALTER FUNCTION photo.guest_volume_collection_current(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guest_volume_collection_current(uuid,uuid) FROM PUBLIC;
-- Deliberately private: only the existing authorized SECURITY DEFINER paths
-- may inspect this tenant/collection pair.
DO $$
DECLARE definition text;anchor text;replacement text;signature text;
BEGIN
 signature:='photo.create_portal_download_intent(bytea,bytea,text,text,uuid,integer,uuid,bytea,timestamptz)';
 SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
 anchor:='INSERT INTO photo.idempotency_records(';
 replacement:=$patch$IF photo.guest_volume_collection_current(ident.tenant_id,ident.collection_id) IS DISTINCT FROM true THEN
  RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::timestamptz;RETURN;
 END IF;
 INSERT INTO photo.idempotency_records($patch$;
 IF definition IS NULL OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1
  OR strpos(definition,'photo.guest_volume_collection_current(')>0
  OR strpos(definition,'photo.authorize_portal_session(')=0 THEN RAISE EXCEPTION 'GUEST_VOLUME_INTENT_PUBLICATION_SOURCE';END IF;
 EXECUTE replace(definition,anchor,replacement);
 FOREACH signature IN ARRAY ARRAY[
  'photo.redeem_download_intent(bytea,text)',
  'photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)'
 ] LOOP
  SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
  anchor:='s.collection_id IS DISTINCT FROM j.collection_id';
  replacement:='photo.guest_volume_collection_current(i.tenant_id,j.collection_id) IS DISTINCT FROM true OR s.collection_id IS DISTINCT FROM j.collection_id';
  IF definition IS NULL OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1
   OR strpos(definition,'photo.guest_volume_collection_current(')>0
   OR strpos(definition,'v30_delivery_download_target')=0 THEN RAISE EXCEPTION 'GUEST_VOLUME_REDEEM_PUBLICATION_SOURCE: %',signature;END IF;
  EXECUTE replace(definition,anchor,replacement);
 END LOOP;
END $$;
-- +goose StatementEnd
-- Existing routine owners and EXECUTE ACLs are preserved by CREATE OR REPLACE.
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
