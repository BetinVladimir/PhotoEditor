-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.list_portal_events(p_session_hash bytea,p_after bigint,p_limit integer)
RETURNS TABLE(outcome text,cursor bigint,type text,payload jsonb,created_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
DECLARE ident record;minimum bigint;emitted boolean:=false;
BEGIN
 IF octet_length(p_session_hash)<>32 OR p_after<0 OR p_limit NOT BETWEEN 1 AND 100 THEN RETURN QUERY SELECT 'invalid',NULL::bigint,NULL::text,NULL::jsonb,NULL::timestamptz;RETURN;END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,NULL,'session');
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::bigint,NULL::text,NULL::jsonb,NULL::timestamptz;RETURN;END IF;
 SELECT min(e.cursor) INTO minimum FROM photo.run_events e WHERE e.tenant_id=ident.tenant_id AND e.collection_id=ident.collection_id AND e.type IN ('collection_changed','upload_status','download_status');
 IF p_after>0 AND minimum IS NOT NULL AND p_after<minimum-1 THEN RETURN QUERY SELECT 'expired',NULL::bigint,NULL::text,NULL::jsonb,NULL::timestamptz;RETURN;END IF;
 FOR cursor,type,payload,created_at IN SELECT e.cursor,e.type,e.payload,e.created_at FROM photo.run_events e
  WHERE e.tenant_id=ident.tenant_id AND e.collection_id=ident.collection_id AND e.cursor>p_after
  AND e.type IN ('collection_changed','upload_status','download_status')
  AND (e.principal_id IS NULL OR e.principal_id=ident.session_id) ORDER BY e.cursor LIMIT p_limit
 LOOP outcome:='ok';emitted:=true;RETURN NEXT;END LOOP;
 IF NOT emitted THEN RETURN QUERY SELECT 'ok',NULL::bigint,NULL::text,NULL::jsonb,NULL::timestamptz;END IF;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.list_portal_events(bytea,bigint,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.list_portal_events(bytea,bigint,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.list_portal_events(bytea,bigint,integer) TO photo_portal;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
