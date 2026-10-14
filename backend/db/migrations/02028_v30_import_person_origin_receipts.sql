-- +goose Up
-- The producer receipt is independent of the user's revocable mask decision.
-- No service role can insert evidence until signature and source admission are
-- wired into a dedicated trusted recorder. A parsed manifest is never a key.
-- +goose StatementBegin
CREATE TABLE photo.v30_import_person_origin_receipts(
 tenant_id uuid NOT NULL,
 workflow_id uuid NOT NULL,
 project_id uuid NOT NULL,
 entry_id uuid NOT NULL,
 source_version_id uuid NOT NULL,
 target_version_id uuid NOT NULL,
 recipe_id uuid NOT NULL,
 recipe_sha text NOT NULL CHECK(recipe_sha ~ '^[0-9a-f]{64}$'),
 override_id uuid NOT NULL,
 override_sha text NOT NULL CHECK(override_sha ~ '^[0-9a-f]{64}$'),
 mask_ref_id uuid NOT NULL,
 mask_ref_sha text NOT NULL CHECK(mask_ref_sha ~ '^[0-9a-f]{64}$'),
 mask_pixels_id uuid NOT NULL,
 mask_pixels_sha text NOT NULL CHECK(mask_pixels_sha ~ '^[0-9a-f]{64}$'),
 receipt_artifact_id uuid NOT NULL,
 receipt_sha text NOT NULL CHECK(receipt_sha ~ '^[0-9a-f]{64}$'),
 issuer_id uuid NOT NULL,
 key_id text NOT NULL CHECK(key_id ~ '^[0-9a-f]{64}$'),
 producer_task_id uuid NOT NULL,
 producer_attempt integer NOT NULL CHECK(producer_attempt>0),
 producer_fence bigint NOT NULL CHECK(producer_fence>0),
 qualified_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,workflow_id,entry_id,override_id,mask_ref_id,target_version_id),
 UNIQUE(tenant_id,workflow_id,receipt_artifact_id),
 FOREIGN KEY(tenant_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id),
 FOREIGN KEY(tenant_id,target_version_id) REFERENCES photo.asset_versions(tenant_id,id),
 FOREIGN KEY(tenant_id,receipt_artifact_id) REFERENCES photo.artifacts(tenant_id,id)
);
ALTER TABLE photo.v30_import_person_origin_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_import_person_origin_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_import_person_origin_receipts
 TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v30_import_person_origin_receipts
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT SELECT,INSERT,DELETE ON photo.v30_import_person_origin_receipts TO photo_fn_owner;

CREATE FUNCTION photo.v30_import_person_origin_receipt_live(
 p_tenant uuid,p_workflow uuid,p_entry uuid,p_target uuid,
 p_recipe uuid,p_recipe_sha text,p_override uuid,p_override_sha text,
 p_mask_ref uuid,p_mask_ref_sha text,p_mask_pixels uuid,p_mask_pixels_sha text,
 p_receipt uuid,p_receipt_sha text,p_parsed text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;record photo.v30_import_person_origin_receipts%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR coalesce(p_recipe_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_override_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_mask_ref_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_mask_pixels_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_receipt_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(sources->'entries') e
  WHERE e->>'entry_id'=p_entry::text AND e->>'state'='adopted'
  AND e->>'asset_version_id'=p_target::text) THEN RETURN false;END IF;
 SELECT * INTO record FROM photo.v30_import_person_origin_receipts r
 WHERE r.tenant_id=p_tenant AND r.workflow_id=p_workflow
 AND r.project_id=(sources->>'project_id')::uuid AND r.entry_id=p_entry
 AND r.target_version_id=p_target AND r.recipe_id=p_recipe AND r.recipe_sha=p_recipe_sha
 AND r.override_id=p_override AND r.override_sha=p_override_sha
 AND r.mask_ref_id=p_mask_ref AND r.mask_ref_sha=p_mask_ref_sha
 AND r.mask_pixels_id=p_mask_pixels AND r.mask_pixels_sha=p_mask_pixels_sha
 AND r.receipt_artifact_id=p_receipt AND r.receipt_sha=p_receipt_sha FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(p_parsed::jsonb->'manifest'->'person_origins','[]'::jsonb)) origin
  JOIN LATERAL jsonb_array_elements(coalesce(p_parsed::jsonb->'manifest'->'inventory','[]'::jsonb)) item
   ON item->>'path'=origin->>'path' AND item->>'contract'='PersonOriginReceipt'
  JOIN LATERAL jsonb_array_elements(coalesce(p_parsed::jsonb->'files','[]'::jsonb)) file
   ON file->>'path'=origin->>'path' AND file->>'contract'='PersonOriginReceipt'
  WHERE origin->>'entry_id'=p_entry::text
   AND origin#>>'{recipe,artifact_id}'=p_recipe::text AND origin#>>'{recipe,sha256}'=p_recipe_sha
   AND origin#>>'{override,artifact_id}'=p_override::text AND origin#>>'{override,sha256}'=p_override_sha
   AND origin#>>'{mask_ref,artifact_id}'=p_mask_ref::text AND origin#>>'{mask_ref,sha256}'=p_mask_ref_sha
   AND origin#>>'{mask_pixels,artifact_id}'=p_mask_pixels::text AND origin#>>'{mask_pixels,sha256}'=p_mask_pixels_sha
   AND origin->>'sha256'=p_receipt_sha AND item->>'sha256'=p_receipt_sha
   AND file#>>'{content,artifact_id}'=p_receipt::text AND file#>>'{content,sha256}'=p_receipt_sha) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=record.receipt_artifact_id AND a.sha256=record.receipt_sha
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.output_role='imported_file' AND a.media_type='application/octet-stream'
  AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_import_person_origin_receipt_live(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_person_origin_receipt_live(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_person_origin_receipt_live(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text)
 TO photo_scheduler;

DO $$ DECLARE definition text;BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 IF definition IS NULL OR position('tables_to_purge constant text[]:=ARRAY[' IN definition)=0
 OR position('''v30_import_person_origin_receipts''' IN definition)>0 THEN
  RAISE EXCEPTION 'V30_PERSON_ORIGIN_PURGE_ALLOWLIST_DRIFT';END IF;
 EXECUTE replace(definition,'tables_to_purge constant text[]:=ARRAY[',
  'tables_to_purge constant text[]:=ARRAY[''v30_import_person_origin_receipts'',');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
