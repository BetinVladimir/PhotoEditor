-- +goose Up
-- The donor decision function must repeat the presentation gate under its
-- own transaction; a client loading the pair is not sufficient authority.
-- +goose StatementBegin
DO $$ DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef('photo.decide_v28_open_eyes_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)'::regprocedure);
 needle:='photo.v28_open_eyes_review_decision_ready(p_tenant,p_review,p_actor,p_session)';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'OPEN_EYES_REVIEW_GATE_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,
  '(photo.v28_open_eyes_review_decision_ready(p_tenant,p_review,p_actor,p_session) AND photo.v28_open_eyes_review_preview_ready(p_tenant,p_review))');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
