-- +goose Up
-- Revision-bound metadata pages; never exposes storage locations or signed URLs.
-- +goose StatementBegin
CREATE FUNCTION photo.get_v30_delivery_items(p_tenant uuid,p_actor uuid,p_session uuid,p_package uuid,
 p_revision bigint,p_after_ordinal integer,p_after_preset uuid,p_limit integer) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.delivery_packages; resource jsonb; answer jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_revision IS NULL OR p_revision<1 OR p_revision>9007199254740991
  OR p_limit IS NULL OR p_limit<1 OR p_limit>200
  OR p_after_ordinal IS NULL OR p_after_ordinal< -1 OR p_after_ordinal>=5000
  OR p_after_preset IS NULL
  OR (p_after_ordinal=-1 AND p_after_preset<>'00000000-0000-0000-0000-000000000000'::uuid)
  OR (p_after_ordinal>=0 AND p_after_preset='00000000-0000-0000-0000-000000000000'::uuid) THEN RETURN NULL; END IF;
 -- Serialize a page with adoption/publication, both of which lock the package.
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=p_tenant AND id=p_package FOR SHARE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 resource:=photo.get_v30_delivery(p_tenant,p_actor,p_session,p_package);
 IF resource IS NULL THEN RETURN NULL; END IF;
 IF d.revision<>p_revision THEN RETURN jsonb_build_object('stale',true); END IF;
 WITH page AS MATERIALIZED (
  SELECT i.id,i.ordinal,i.preset_id,i.preset_sha,i.required,i.export_id,i.state,i.reason
  FROM photo.delivery_items i WHERE i.tenant_id=p_tenant AND i.package_id=p_package
   AND (i.ordinal,i.preset_id)>(p_after_ordinal,p_after_preset)
  ORDER BY i.ordinal,i.preset_id LIMIT p_limit+1
 ), visible AS MATERIALIZED (SELECT * FROM page ORDER BY ordinal,preset_id LIMIT p_limit)
 SELECT jsonb_build_object('package_id',d.id,'revision',d.revision::text,
  'manifest',resource->'document',
  'items',coalesce((SELECT jsonb_agg(to_jsonb(v) ORDER BY v.ordinal,v.preset_id) FROM visible v),'[]'::jsonb),
  'next',CASE WHEN (SELECT count(*) FROM page)>p_limit THEN
   (SELECT jsonb_build_object('ordinal',v.ordinal,'preset_id',v.preset_id) FROM visible v ORDER BY v.ordinal DESC,v.preset_id DESC LIMIT 1)
   ELSE NULL END) INTO answer;
 RETURN answer;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.get_v30_delivery_items(uuid,uuid,uuid,uuid,bigint,integer,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_v30_delivery_items(uuid,uuid,uuid,uuid,bigint,integer,uuid,integer) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.get_v30_delivery_items(uuid,uuid,uuid,uuid,bigint,integer,uuid,integer) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
