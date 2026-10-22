-- +goose Up
ALTER TABLE photo.download_intents ADD COLUMN url_ttl_seconds integer NOT NULL DEFAULT 60 CHECK(url_ttl_seconds BETWEEN 1 AND 60);
-- +goose StatementBegin
CREATE FUNCTION photo.download_policy_seconds(p_config jsonb,p_key text) RETURNS integer
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE p jsonb;v integer;cap integer;
BEGIN
 cap:=CASE p_key WHEN 'retention_seconds' THEN 86400 WHEN 'link_seconds' THEN 60 ELSE NULL END;
 IF cap IS NULL THEN RAISE EXCEPTION 'DOWNLOAD_POLICY_KEY';END IF;
 IF NOT (p_config ? 'download_policy') THEN RETURN cap;END IF;
 p:=p_config->'download_policy';
 IF jsonb_typeof(p) IS DISTINCT FROM 'object' OR NOT p ?& ARRAY['retention_seconds','link_seconds'] OR (SELECT count(*) FROM jsonb_object_keys(p))<>2
 OR jsonb_typeof(p->p_key) IS DISTINCT FROM 'number' OR (p->>p_key) !~ '^[1-9][0-9]{0,4}$' THEN RAISE EXCEPTION 'DOWNLOAD_POLICY_INVALID';END IF;
 v:=(p->>p_key)::integer;IF v>cap THEN RAISE EXCEPTION 'DOWNLOAD_POLICY_INVALID';END IF;RETURN v;
END $$;
CREATE FUNCTION photo.download_link_seconds(p_config jsonb) RETURNS integer LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,photo AS $$ SELECT photo.download_policy_seconds(p_config,'link_seconds') $$;
CREATE FUNCTION photo.download_policy_immutable() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.kind='download' AND OLD.config_snapshot->'download_policy' IS DISTINCT FROM NEW.config_snapshot->'download_policy' THEN RAISE EXCEPTION 'DOWNLOAD_POLICY_IMMUTABLE';END IF;RETURN NEW;
END $$;
CREATE TRIGGER download_policy_immutable BEFORE UPDATE OF config_snapshot ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.download_policy_immutable();
CREATE FUNCTION photo.download_intent_ttl_immutable() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.url_ttl_seconds IS DISTINCT FROM NEW.url_ttl_seconds THEN RAISE EXCEPTION 'DOWNLOAD_POLICY_IMMUTABLE';END IF;RETURN NEW;
END $$;
CREATE TRIGGER download_intent_ttl_immutable BEFORE UPDATE OF url_ttl_seconds ON photo.download_intents FOR EACH ROW EXECUTE FUNCTION photo.download_intent_ttl_immutable();
-- Preserve all latest source, attempt, archive and revocation fences; only alter
-- the policy inputs at admission/atomic finalization. Missing anchors fail deployment.
DO $$
DECLARE def text;anchor text;replacement text;
BEGIN
 def:=pg_get_functiondef('photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid)'::regprocedure);
 anchor:=$a$'failure_policy','continue_items','scope','selected');$a$;
 replacement:=$a$'failure_policy','continue_items','scope','selected','download_policy',jsonb_build_object('retention_seconds',coalesce(nullif(current_setting('photo.download_retention_seconds',true),''),'86400')::integer,'link_seconds',coalesce(nullif(current_setting('photo.download_link_seconds',true),''),'60')::integer));$a$;
 IF strpos(def,anchor)=0 THEN RAISE EXCEPTION 'DOWNLOAD_START_TTL_ANCHOR';END IF;EXECUTE replace(def,anchor,replacement);
 def:=pg_get_functiondef('photo.finalize_download_archives(uuid,uuid)'::regprocedure);
 anchor:=$a$until_time:=clock_timestamp()+interval '24 hours';$a$;
 IF strpos(def,anchor)=0 THEN RAISE EXCEPTION 'DOWNLOAD_FINALIZE_TTL_ANCHOR';END IF;
 EXECUTE replace(def,anchor,$a$until_time:=clock_timestamp()+make_interval(secs=>photo.download_policy_seconds(r.config_snapshot,'retention_seconds'));$a$);
 def:=pg_get_functiondef('photo.seal_download_archives(uuid,uuid)'::regprocedure);
 anchor:=$a$until_time:=clock_timestamp()+interval '24 hours';$a$;
 IF strpos(def,anchor)=0 THEN RAISE EXCEPTION 'DOWNLOAD_SEAL_TTL_ANCHOR';END IF;
 EXECUTE replace(def,anchor,$a$until_time:=clock_timestamp()+make_interval(secs=>photo.download_policy_seconds(r.config_snapshot,'retention_seconds'));$a$);
 def:=pg_get_functiondef('photo.create_portal_download_intent(bytea,bytea,text,text,uuid,integer,uuid,bytea,timestamptz)'::regprocedure);
 anchor:='token_hash,expires_at)';IF strpos(def,anchor)=0 THEN RAISE EXCEPTION 'DOWNLOAD_INTENT_TTL_ANCHOR';END IF;
 def:=replace(def,anchor,'token_hash,expires_at,url_ttl_seconds)');
 anchor:='p_token_hash,least(p_expires,j.expires_at))';
 IF strpos(def,anchor)=0 THEN RAISE EXCEPTION 'DOWNLOAD_INTENT_VALUE_ANCHOR';END IF;
 def:=replace(def,anchor,$a$p_token_hash,least(p_expires,j.expires_at,clock_timestamp()+make_interval(secs=>(SELECT photo.download_link_seconds(config_snapshot) FROM photo.runs WHERE tenant_id=j.tenant_id AND id=j.run_id))),(SELECT photo.download_link_seconds(config_snapshot) FROM photo.runs WHERE tenant_id=j.tenant_id AND id=j.run_id))$a$);
 -- Response and idempotent receipt use the actual clamped row expiry.
 def:=replace(def,'least(p_expires,j.expires_at)', '(SELECT x.expires_at FROM photo.download_intents x WHERE x.tenant_id=j.tenant_id AND x.id=p_intent)');EXECUTE def;
 def:=pg_get_functiondef('photo.create_portal_item_download_intent(bytea,bytea,text,text,uuid,uuid,uuid,bytea,timestamptz)'::regprocedure);
 anchor:='expires_at,intent_kind,item_id,preset_id)';IF strpos(def,anchor)=0 THEN RAISE EXCEPTION 'DOWNLOAD_ITEM_TTL_ANCHOR';END IF;
 def:=replace(def,anchor,'expires_at,intent_kind,item_id,preset_id,url_ttl_seconds)');
 anchor:=$a$until_time,'item',i.id,pr.id)$a$;IF strpos(def,anchor)=0 THEN RAISE EXCEPTION 'DOWNLOAD_ITEM_VALUE_ANCHOR';END IF;
 EXECUTE replace(def,anchor,$a$until_time,'item',i.id,pr.id,coalesce(nullif(current_setting('photo.download_link_seconds',true),''),'60')::integer)$a$);
END $$;
CREATE FUNCTION photo.start_portal_download_policy(a bytea,b bytea,c text,d text,e uuid,f uuid,g uuid,h uuid,retention integer,link integer)
RETURNS TABLE(outcome text,job_id uuid,status text) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF retention IS NULL OR retention NOT BETWEEN 1 AND 86400 OR link IS NULL OR link NOT BETWEEN 1 AND 60 THEN RAISE EXCEPTION 'DOWNLOAD_POLICY_INVALID';END IF;
 PERFORM set_config('photo.download_retention_seconds',retention::text,true);PERFORM set_config('photo.download_link_seconds',link::text,true);
 RETURN QUERY SELECT * FROM photo.start_portal_download(a,b,c,d,e,f,g,h);
END $$;
CREATE FUNCTION photo.create_portal_item_download_intent_policy(a bytea,b bytea,c text,d text,e uuid,f uuid,g uuid,h bytea,i timestamptz,link integer)
RETURNS TABLE(outcome text,intent_id uuid,expires_at timestamptz) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF link IS NULL OR link NOT BETWEEN 1 AND 60 THEN RAISE EXCEPTION 'DOWNLOAD_POLICY_INVALID';END IF;
 PERFORM set_config('photo.download_link_seconds',link::text,true);
 RETURN QUERY SELECT * FROM photo.create_portal_item_download_intent(a,b,c,d,e,f,g,h,least(i,clock_timestamp()+make_interval(secs=>link)));
END $$;
CREATE FUNCTION photo.download_intent_signing_seconds(h bytea,b text,k text,v text,s text,n bigint,m text) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result integer;tid uuid;previous_tenant text;
BEGIN
 IF photo.v30_delivery_download_intent_current(h,b,k,v,s,n,m) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT i.tenant_id INTO tid FROM photo.download_intents i WHERE i.token_hash=h AND i.consumed_at IS NOT NULL;
 IF tid IS NULL THEN RETURN NULL;END IF;previous_tenant:=current_setting('app.tenant_id',true);PERFORM set_config('app.tenant_id',tid::text,true);
 SELECT least(i.url_ttl_seconds,
 coalesce(floor(extract(epoch FROM (a.expires_at-clock_timestamp())))::integer,60)) INTO result
 FROM photo.download_intents i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.token_hash=h AND i.consumed_at IS NOT NULL AND a.bucket=b AND a.object_key=k AND a.object_version=v AND a.sha256=s AND a.bytes=n AND a.media_type=m;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 IF result IS NULL OR result NOT BETWEEN 1 AND 60 THEN RETURN NULL;END IF;RETURN result;
END $$;
ALTER FUNCTION photo.download_policy_seconds(jsonb,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.download_link_seconds(jsonb) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.download_policy_immutable() OWNER TO photo_fn_owner;
ALTER FUNCTION photo.download_intent_ttl_immutable() OWNER TO photo_fn_owner;
ALTER FUNCTION photo.start_portal_download_policy(bytea,bytea,text,text,uuid,uuid,uuid,uuid,integer,integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.create_portal_item_download_intent_policy(bytea,bytea,text,text,uuid,uuid,uuid,bytea,timestamptz,integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.download_intent_signing_seconds(bytea,text,text,text,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_policy_seconds(jsonb,text),photo.download_link_seconds(jsonb),photo.download_policy_immutable(),photo.download_intent_ttl_immutable(),photo.start_portal_download_policy(bytea,bytea,text,text,uuid,uuid,uuid,uuid,integer,integer),photo.create_portal_item_download_intent_policy(bytea,bytea,text,text,uuid,uuid,uuid,bytea,timestamptz,integer),photo.download_intent_signing_seconds(bytea,text,text,text,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.download_policy_seconds(jsonb,text),photo.download_link_seconds(jsonb) TO photo_api;
GRANT EXECUTE ON FUNCTION photo.start_portal_download_policy(bytea,bytea,text,text,uuid,uuid,uuid,uuid,integer,integer),photo.create_portal_item_download_intent_policy(bytea,bytea,text,text,uuid,uuid,uuid,bytea,timestamptz,integer),photo.download_intent_signing_seconds(bytea,text,text,text,text,bigint,text) TO photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
