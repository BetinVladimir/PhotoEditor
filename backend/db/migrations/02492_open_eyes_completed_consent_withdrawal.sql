-- +goose Up
-- OpenEyes is independent of optional Glare worker/adoption enablement.
ALTER TABLE photo.runs ADD COLUMN v28_donor_authority_failure jsonb
 CHECK(v28_donor_authority_failure IS NULL OR jsonb_typeof(v28_donor_authority_failure)='object');
-- +goose StatementBegin
DO $$ DECLARE definition text;anchor text;BEGIN
 definition:=pg_get_functiondef('photo.guard_glare_authority_failure()'::regprocedure);
 definition:=replace(definition,'guard_glare_authority_failure','guard_donor_authority_failure');
 definition:=replace(definition,'v28_glare_authority_failure','v28_donor_authority_failure');
 definition:=replace(definition,'GLARE_AUTHORITY_FAILURE_IMMUTABLE','OPEN_EYES_AUTHORITY_FAILURE_IMMUTABLE');EXECUTE definition;
 definition:=pg_get_functiondef('photo.completed_glare_consent_loss(uuid,uuid,uuid)'::regprocedure);
 definition:=replace(definition,'completed_glare_consent_loss','completed_open_eyes_consent_loss');
 definition:=replace(definition,'portrait.glare','portrait.open_eyes');
 definition:=replace(definition,'v28_restoration_descriptor_jcs','v28_donor_descriptor_jcs');
 definition:=replace(definition,'r.v28_restoration_session_id',$a$(r.v28_donor_queued_receipt->>'auth_session_id')::uuid$a$);
 definition:=replace(definition,'GLARE_CONSENT_REVOKED','OPEN_EYES_CONSENT_REVOKED');
 definition:=replace(definition,'GLARE_SOURCE_CONFIRMATION_WITHDRAWN','OPEN_EYES_SOURCE_CONFIRMATION_WITHDRAWN');
 anchor:=$a$OR coalesce(body->>'mode','') NOT IN('generative','donor')$a$;
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'OPEN_EYES_WITHDRAWAL_MODE_DRIFT';END IF;
 definition:=replace(definition,anchor,$a$OR body->>'schema_version' IS DISTINCT FROM '1'
 OR r.v28_donor_queued_receipt->>'descriptor_sha' IS DISTINCT FROM encode(sha256(convert_to(r.v28_donor_descriptor_jcs,'UTF8')),'hex')
 OR r.v28_donor_queued_receipt->>'task_id' IS DISTINCT FROM t.id::text
 OR r.v28_donor_queued_receipt->>'actor_id' IS DISTINCT FROM r.created_by::text$a$);
 definition:=replace(definition,$a$doc.purpose=CASE body->>'mode' WHEN 'generative' THEN 'v28_glare_source_confirmation' ELSE 'v28_donor_confirmation' END$a$,
  $a$doc.purpose='v28_donor_confirmation'$a$);
 definition:=replace(definition,$a$c:=CASE body->>'mode' WHEN 'generative' THEN d.v28_glare_source_issuer_context ELSE d.v28_donor_issuer_context END$a$,
  $a$c:=d.v28_donor_issuer_context$a$);
 anchor:=$a$(CASE body->>'mode' WHEN 'generative' THEN c->'params'->'glasses_mask' ELSE c->'params'->'eyes_mask' END) IS DISTINCT FROM body->'glasses_mask'$a$;
 IF position(anchor in definition)=0 THEN RAISE EXCEPTION 'OPEN_EYES_WITHDRAWAL_MASK_DRIFT';END IF;
 definition:=replace(definition,anchor,$a$c->'params'->'eyes_mask' IS DISTINCT FROM body->'eyes_mask'$a$);
 anchor:=$a$IF body->>'mode'='generative' AND ((c->'params')-'confirmation') IS DISTINCT FROM (body-'confirmation') THEN RETURN NULL;END IF;$a$;
 IF position(anchor in definition)=0 THEN RAISE EXCEPTION 'OPEN_EYES_WITHDRAWAL_PARAMS_DRIFT';END IF;
 definition:=replace(definition,anchor,$a$IF ((c->'params')-'confirmation') IS DISTINCT FROM (body-'confirmation') THEN RETURN NULL;END IF;$a$);
 IF position('v28_glare_source_issuer_context' in definition)>0 THEN RAISE EXCEPTION 'OPEN_EYES_WITHDRAWAL_SOURCE_BRANCH_REMAINED';END IF;
 EXECUTE definition;

 definition:=pg_get_functiondef('photo.withdraw_completed_glare_consent()'::regprocedure);
 definition:=replace(definition,'withdraw_completed_glare_consent','withdraw_completed_open_eyes_consent');
 definition:=replace(definition,'completed_glare_consent_loss','completed_open_eyes_consent_loss');
 definition:=replace(definition,'portrait.glare','portrait.open_eyes');
 definition:=replace(definition,'v28_glare_authority_failure','v28_donor_authority_failure');
 definition:=replace(definition,'v28_restoration_descriptor_jcs','v28_donor_descriptor_jcs');
 definition:=replace(definition,'v28_restoration_preview_task_id','v26_derivative_task_id');
 definition:=replace(definition,'glare:consent:withdrawal','open-eyes:consent:withdrawal');
 anchor:=$a$((CASE (r.v28_donor_descriptor_jcs::jsonb)->'params'->>'mode' WHEN 'generative'
  THEN d.v28_glare_source_issuer_context ELSE d.v28_donor_issuer_context END)->>'proof_jcs')::jsonb$a$;
 IF position(anchor in definition)=0 THEN RAISE EXCEPTION 'OPEN_EYES_WITHDRAWAL_PROOF_DRIFT';END IF;
 definition:=replace(definition,anchor,$a$(d.v28_donor_issuer_context->>'proof_jcs')::jsonb$a$);EXECUTE definition;

 definition:=pg_get_functiondef('photo.glare_withdrawn_quota_settled(uuid,uuid)'::regprocedure);
 definition:=replace(definition,'glare_withdrawn_quota_settled','open_eyes_withdrawn_quota_settled');
 definition:=replace(definition,'v28_glare_authority_failure','v28_donor_authority_failure');
 definition:=replace(definition,'v28_restoration_preview_task_id','v26_derivative_task_id');EXECUTE definition;
 definition:=pg_get_functiondef('photo.release_next_glare_withdrawn_quota()'::regprocedure);
 definition:=replace(definition,'release_next_glare_withdrawn_quota','release_next_open_eyes_withdrawn_quota');
 definition:=replace(definition,'glare_withdrawn_quota_settled','open_eyes_withdrawn_quota_settled');
 definition:=replace(definition,'GLARE_WITHDRAWN_QUOTA_INCONSISTENT','OPEN_EYES_WITHDRAWN_QUOTA_INCONSISTENT');EXECUTE definition;
END $$;
ALTER FUNCTION photo.guard_donor_authority_failure() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_donor_authority_failure() FROM PUBLIC;
CREATE TRIGGER runs_donor_authority_failure_guard BEFORE INSERT OR UPDATE OF v28_donor_authority_failure
 ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_donor_authority_failure();
ALTER FUNCTION photo.completed_open_eyes_consent_loss(uuid,uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.withdraw_completed_open_eyes_consent() OWNER TO photo_fn_owner;
ALTER FUNCTION photo.open_eyes_withdrawn_quota_settled(uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.release_next_open_eyes_withdrawn_quota() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.completed_open_eyes_consent_loss(uuid,uuid,uuid),photo.open_eyes_withdrawn_quota_settled(uuid,uuid)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_portal,photo_gateway,photo_maintenance;
REVOKE ALL ON FUNCTION photo.withdraw_completed_open_eyes_consent(),photo.release_next_open_eyes_withdrawn_quota()
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.withdraw_completed_open_eyes_consent(),photo.release_next_open_eyes_withdrawn_quota() TO photo_scheduler;

DO $$ DECLARE definition text;anchor text;BEGIN
 definition:=pg_get_functiondef('photo.guard_v28_open_eyes_review_update()'::regprocedure);
 anchor:=' IF OLD.kind=''portrait.open_eyes'' OR NEW.kind=''portrait.open_eyes'' THEN';
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'OPEN_EYES_CANCEL_GUARD_DRIFT';END IF;
 EXECUTE replace(definition,anchor,$a$ IF OLD.kind='portrait.open_eyes' AND OLD.state='pending' AND NEW.state='cancelled' THEN
  IF current_user<>'photo_fn_owner' OR NEW.revision<>OLD.revision+1
   OR (to_jsonb(NEW)-'state'-'revision') IS DISTINCT FROM (to_jsonb(OLD)-'state'-'revision')
   OR NOT EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=OLD.tenant_id AND r.id=OLD.run_id AND r.cancel_requested AND r.status='cancelling')
  THEN RAISE EXCEPTION 'OPEN_EYES_CANCEL_AUTHORITY' USING ERRCODE='42501';END IF;RETURN NEW;
 END IF;
$a$||anchor);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '02492 is forward-only';END $$;
-- +goose StatementEnd
