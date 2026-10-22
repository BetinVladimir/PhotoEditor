-- +goose Up
CREATE TABLE photo.export_delivery_batches(
 tenant_id uuid NOT NULL, id uuid NOT NULL, actor_id uuid NOT NULL,
 project_id uuid NOT NULL, request_key text NOT NULL,
 request_sha text NOT NULL CHECK(request_sha ~ '^[0-9a-f]{64}$'),
 plan jsonb NOT NULL CHECK(jsonb_typeof(plan)='array' AND jsonb_array_length(plan) BETWEEN 1 AND 3),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,actor_id,request_key),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id)
);
CREATE TABLE photo.export_delivery_tickets(
 tenant_id uuid NOT NULL,batch_id uuid NOT NULL,group_key text NOT NULL,
 ticket jsonb CHECK(jsonb_typeof(ticket)='object'),
 failure_code text CHECK(failure_code ~ '^[A-Z][A-Z0-9_]{0,79}$'),
 CHECK((ticket IS NULL)<>(failure_code IS NULL)),
 PRIMARY KEY(tenant_id,batch_id,group_key),
 FOREIGN KEY(tenant_id,batch_id) REFERENCES photo.export_delivery_batches(tenant_id,id)
);
ALTER TABLE photo.export_delivery_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.export_delivery_batches FORCE ROW LEVEL SECURITY;
ALTER TABLE photo.export_delivery_tickets ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.export_delivery_tickets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.export_delivery_batches TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY tenant_scope ON photo.export_delivery_tickets TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.export_delivery_batches TO photo_fn_owner USING(true) WITH CHECK(true);
CREATE POLICY trusted_functions ON photo.export_delivery_tickets TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.export_delivery_batches,photo.export_delivery_tickets TO photo_api;
GRANT SELECT,DELETE ON photo.export_delivery_batches,photo.export_delivery_tickets TO photo_fn_owner;
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 needle:=' DELETE FROM photo.editor_effect_builds WHERE tenant_id=j.tenant_id;';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'EXPORT_BATCH_PURGE_ANCHOR_MISSING';END IF;
 EXECUTE replace(definition,needle,' DELETE FROM photo.export_delivery_tickets WHERE tenant_id=j.tenant_id; DELETE FROM photo.export_delivery_batches WHERE tenant_id=j.tenant_id;'||needle);
END $$;
-- +goose StatementEnd
-- Exact frozen-preset naming for Delivery slots. File authority is already
-- rechecked by redeem_download_intent; this private reader supplies no URL.
-- +goose StatementBegin
CREATE FUNCTION photo.delivery_export_basename(p_tenant uuid,p_package uuid,p_ordinal integer,p_preset uuid,p_sha text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proof jsonb;config_jcs text;original_name text;pin text;
BEGIN
 SELECT doc.v30_delivery_proof INTO proof FROM photo.delivery_packages d
 JOIN photo.v2_workflows w ON w.tenant_id=d.tenant_id AND w.id=d.workflow_id
 JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id AND t.id=photo.v30_uuid5(w.id,'v2.delivery.prepare:0') AND t.state='succeeded'
 JOIN photo.backend_documents doc ON doc.tenant_id=t.tenant_id AND doc.run_id=t.run_id AND doc.owner_id=t.id AND doc.artifact_id=t.params_artifact_id AND doc.purpose='task_params' AND doc.state='ready'
 WHERE d.tenant_id=p_tenant AND d.id=p_package AND d.state='ready' AND d.published_at IS NOT NULL;
 IF proof IS NULL THEN RAISE EXCEPTION 'DELIVERY_NAMING_PROOF_MISSING';END IF;
 SELECT entry->>'config_jcs' INTO config_jcs FROM jsonb_array_elements(proof->'configs_jcs') entry WHERE entry->>'preset_id'=p_preset::text;
 SELECT entry->>'preset_sha' INTO pin FROM jsonb_array_elements((proof->>'frozen_input_jcs')::jsonb->'presets') entry WHERE entry->>'preset_id'=p_preset::text;
 IF config_jcs IS NULL OR pin IS NULL OR encode(sha256(convert_to(config_jcs,'UTF8')),'hex') IS DISTINCT FROM pin THEN RAISE EXCEPTION 'DELIVERY_NAMING_PRESET_INVALID';END IF;
 SELECT coalesce(nullif(si.payload->>'filename',''),u.filename,'photo-'||lpad(p_ordinal::text,4,'0')) INTO original_name
 FROM photo.delivery_packages d JOIN photo.snapshot_items si ON si.tenant_id=d.tenant_id AND si.snapshot_id=d.snapshot_id AND si.ordinal=p_ordinal
 JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 LEFT JOIN photo.upload_sessions u ON u.tenant_id=a.tenant_id AND u.id=CASE WHEN a.provenance->>'upload_id' ~ '^[0-9a-f-]{36}$' THEN (a.provenance->>'upload_id')::uuid END
 WHERE d.tenant_id=p_tenant AND d.id=p_package;
 IF original_name IS NULL THEN RAISE EXCEPTION 'DELIVERY_NAMING_SOURCE_MISSING';END IF;
 RETURN left(regexp_replace(photo.download_export_basename(original_name,p_sha,p_ordinal,(config_jcs::jsonb)->>'naming_template'),'[/\\[:cntrl:]]','_','g'),180);
END $$;
ALTER FUNCTION photo.delivery_export_basename(uuid,uuid,integer,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.delivery_export_basename(uuid,uuid,integer,uuid,text) FROM PUBLIC,photo_api,photo_portal,photo_worker,photo_scheduler;
DO $$ DECLARE definition text;
 needle text:=$old$'delivery-'||left(i.delivery_package_id::text,8)||
   CASE WHEN i.delivery_kind='slot' THEN '-'||lpad(i.delivery_ordinal::text,4,'0') ELSE '-manifest' END||ext$old$;
 replacement text:=$new$CASE WHEN i.delivery_kind='slot' THEN photo.delivery_export_basename(i.tenant_id,i.delivery_package_id,i.delivery_ordinal,i.delivery_preset_id,target.sha256) ELSE 'delivery-'||left(i.delivery_package_id::text,8)||'-manifest' END||ext$new$;
BEGIN
 SELECT pg_get_functiondef('photo.redeem_download_intent(bytea,text)'::regprocedure) INTO definition;
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'DELIVERY_NAMING_REDEEM_ANCHOR_MISSING';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'forward-only';END $$;
-- +goose StatementEnd
