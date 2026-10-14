-- +goose Up
-- Reuse the existing one-task Params reservation/physical-version protocol.
-- The required-mode root is a signed envelope; the ordinary off-mode root
-- remains the exact saved assembly, with unchanged bytes and admission.
-- +goose StatementBegin
DO $$ DECLARE definition text;
 needle text:=' raw:=photo.v30_delivery_saved_assembly(p_expansion,p_token,(ref->>''artifact_id'')::uuid,ref->>''sha256'');
 IF raw IS NULL OR photo.retain_v30_delivery_assembly(p_expansion,p_token,(ref->>''artifact_id'')::uuid,ref->>''sha256'') IS DISTINCT FROM true THEN RETURN NULL;END IF;';
 replacement text:=' raw:=photo.v30_delivery_saved_assembly(p_expansion,p_token,(ref->>''artifact_id'')::uuid,ref->>''sha256'');
 IF raw IS NOT NULL THEN
  IF photo.retain_v30_delivery_assembly(p_expansion,p_token,(ref->>''artifact_id'')::uuid,ref->>''sha256'') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 ELSE
  SELECT envelope_jcs INTO raw FROM photo.read_v30_delivery_signed_envelope(p_expansion,p_token,
   (ref->>''artifact_id'')::uuid,ref->>''sha256'');
  IF raw IS NULL THEN RETURN NULL;END IF;
 END IF;';
BEGIN
 SELECT pg_get_functiondef('photo.v30_delivery_assembly_params_scope(uuid,uuid,text)'::regprocedure) INTO definition;
 IF definition IS NULL OR position(needle IN definition)=0
  OR position('read_v30_delivery_signed_envelope' IN definition)>0 THEN
  RAISE EXCEPTION 'DELIVERY_SIGNED_PARAMS_SCOPE_SHAPE';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
