-- +goose Up
-- New children use batch; receipt checks preserve immutable historical omission.
-- +goose StatementBegin
CREATE FUNCTION photo.v32_delivery_child_config(p_tenant uuid,p_project uuid,p_request jsonb,p_export uuid)
RETURNS jsonb LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb; base jsonb;
BEGIN
 IF p_request IS NULL OR p_project IS NULL OR p_export IS NULL THEN RETURN NULL;END IF;
 base:=jsonb_build_object('project_id',p_project,'v30_delivery_export',p_request,'export_id',p_export);
 SELECT config_snapshot INTO saved FROM photo.runs
 WHERE tenant_id=p_tenant AND id=(p_request->>'child_run_id')::uuid;
 IF FOUND AND NOT(saved ? 'queue_class') THEN RETURN base;END IF;
 IF FOUND AND saved->'queue_class' IS DISTINCT FROM '"batch"'::jsonb THEN RETURN NULL;END IF;
 RETURN base||jsonb_build_object('queue_class','batch');
EXCEPTION WHEN invalid_text_representation THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v32_delivery_child_config(uuid,uuid,jsonb,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v32_delivery_child_config(uuid,uuid,jsonb,uuid)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
DO $$
DECLARE signature text; definition text; needle text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
 'photo.create_v30_delivery_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint)',
 'photo.activate_v30_delivery_child(uuid,uuid,integer,text,uuid)',
 'photo.commit_v30_delivery_child(uuid,uuid,integer,text,uuid)',
 'photo.v30_delivery_child_receipt(uuid,uuid,integer,text,uuid,jsonb)'
 ] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  needle:='jsonb_build_object(''project_id'',w.project_id,''v30_delivery_export'',expected,''export_id'',p_export)';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'Unexpected delivery child queue definition: %',signature;
  END IF;
  EXECUTE replace(definition,needle,'photo.v32_delivery_child_config(e.tenant_id,w.project_id,expected,p_export)');
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
