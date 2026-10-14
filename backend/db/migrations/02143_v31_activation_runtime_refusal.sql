-- +goose Up
-- A lost signer-adopter lease after pending pin is an availability failure.
-- Preserve the completed-ticket replay branch and prove registered transport
-- before emitting a distinguishable error. Other refusals remain false.
-- +goose StatementBegin
DO $$
DECLARE definition text;
 gate constant text := ' IF photo.v31_signing_adopter_runtime_ready(saved->>''bucket'',saved->>''params_jcs'') IS DISTINCT FROM true THEN RETURN false;END IF;';
 transport constant text := ' IF photo.v31_signing_transport_ready(p_tenant,p_actor,p_session,p_key,p_request_sha,p_task) IS DISTINCT FROM true THEN RETURN false;END IF;';
BEGIN
 definition:=pg_get_functiondef('photo.activate_v31_signing_pre_session(uuid,uuid,uuid,text,text,jsonb,jsonb)'::regprocedure);
 IF (length(definition)-length(replace(definition,gate,'')))/length(gate)<>1
 OR (length(definition)-length(replace(definition,transport,'')))/length(transport)<>1 THEN
  RAISE EXCEPTION 'Unexpected V31 activation definition';END IF;
 definition:=replace(definition,gate,'');
 definition:=replace(definition,transport,transport||
  ' IF photo.v31_signing_adopter_runtime_ready(saved->>''bucket'',saved->>''params_jcs'') IS DISTINCT FROM true THEN RAISE EXCEPTION ''SIGNING_ADOPTER_UNAVAILABLE'' USING ERRCODE=''P0001'';END IF;');
 EXECUTE definition;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
