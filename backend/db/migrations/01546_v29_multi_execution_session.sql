-- +goose Up
-- Replacement session resolves only through current admitted execution authority.
-- Source mapping, lease, qualification and result checks remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_execution_session(p_tenant uuid,p_run uuid,p_config jsonb)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE previous_tenant text;plan jsonb;answer uuid;
BEGIN
 previous_tenant:=current_setting('app.tenant_id',true);
 IF previous_tenant IS NOT NULL AND previous_tenant<>'' AND previous_tenant<>p_tenant::text THEN RETURN NULL;END IF;
 IF p_config IS NULL THEN RETURN NULL;END IF;
 IF NOT(p_config ? 'v29_negative_replacement_draft_jcs') THEN
 IF p_config ? 'v29_negative_replacement' THEN RETURN NULL;END IF;
 RETURN (p_config->'v29_multi'->>'auth_session_id')::uuid;
 END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 plan:=photo.v29_multi_execution_plan(p_tenant,p_run);
 IF plan IS NOT NULL AND EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.config_snapshot=p_config) THEN
 answer:=(plan->>'auth_session_id')::uuid;
 END IF;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 IF answer='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN NULL;END IF;
 RETURN answer;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_multi_execution_session(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_execution_session(uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;old_session text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
 'photo.v29_worker_multi_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
 'photo.v29_worker_multi_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
 'photo.v29_multi_adoption_evidence(uuid,uuid,integer,bigint,uuid,text)'] LOOP
 definition:=pg_get_functiondef(signature::regprocedure);
 old_session:='(r.config_snapshot->''v29_multi''->>''auth_session_id'')::uuid';
 IF strpos(definition,old_session)>0 THEN
 definition:=replace(definition,old_session,'photo.v29_multi_execution_session(p_tenant,r.id,r.config_snapshot)');
 ELSE
 old_session:='r.config_snapshot->''v29_multi''->>''auth_session_id''';
 IF strpos(definition,old_session)=0 THEN RAISE EXCEPTION 'MULTI_SESSION_BASELINE_MISMATCH: %',signature;END IF;
 definition:=replace(definition,old_session,'photo.v29_multi_execution_session(p_tenant,r.id,r.config_snapshot)::text');
 END IF;
 EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
