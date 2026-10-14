-- +goose Up
-- The prepare source may carry an explicitly frozen required signing policy.
-- This changes no public Create gate and grants no signed assembly admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_frozen_signing_choice_valid(p_policy jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
DECLARE mode text;policy_id uuid;
BEGIN
 IF jsonb_typeof(p_policy) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 mode:=p_policy->>'provenance';
 IF mode='off' THEN
  RETURN NOT (p_policy ? 'signing_policy_id') AND NOT (p_policy ? 'signing_policy_sha');
 END IF;
 IF mode IS DISTINCT FROM 'required'
 OR jsonb_typeof(p_policy->'signing_policy_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_policy->'signing_policy_sha') IS DISTINCT FROM 'string'
 OR (p_policy->>'signing_policy_sha') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 policy_id:=(p_policy->>'signing_policy_id')::uuid;
 RETURN policy_id IS NOT NULL AND policy_id<>'00000000-0000-0000-0000-000000000000'::uuid;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v31_delivery_frozen_signing_choice_valid(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_frozen_signing_choice_valid(jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;

DO $$ DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_delivery_prepare_source_ready_before_tenant_context(uuid,uuid,uuid,uuid,text,text)'::regprocedure);
 needle:='frozen->''policy''->>''provenance'' IS DISTINCT FROM ''off''';
 replacement:='photo.v31_delivery_frozen_signing_choice_valid(frozen->''policy'') IS DISTINCT FROM true';
 IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'DELIVERY_PREPARE_SIGNING_CHOICE_SHAPE';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
