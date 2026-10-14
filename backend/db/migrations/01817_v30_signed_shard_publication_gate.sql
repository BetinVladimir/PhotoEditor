-- +goose Up
-- Required-mode shard publication is allowed only after the retained V31
-- closure passes a fresh whole-matrix check under the same parent lease.
-- Off-mode uses the same gate's unchanged legacy branch. This does not
-- activate a signed assembly task or publish a signed manifest.
-- +goose StatementBegin
DO $$ DECLARE definition text;
 old_clause text:='OR frozen->''policy''->>''provenance'' IS DISTINCT FROM ''off'' THEN RETURN NULL;END IF;';
 new_clause text:='OR photo.v30_delivery_signing_assembly_gate(p_expansion,p_token) IS DISTINCT FROM true THEN RETURN NULL;END IF;';
BEGIN
 SELECT pg_get_functiondef('photo.v30_delivery_shard_scope(uuid,uuid,text,text)'::regprocedure) INTO definition;
 IF definition IS NULL OR position(old_clause IN definition)=0
  OR position('photo.v30_delivery_signing_assembly_gate' IN definition)>0 THEN
  RAISE EXCEPTION 'DELIVERY_SIGNED_SHARD_SCOPE_SHAPE';END IF;
 EXECUTE replace(definition,old_clause,new_clause);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
