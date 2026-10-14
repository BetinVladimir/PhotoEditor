-- +goose Up
-- Extend the producer-lock barrier to every artifact producer, so a transitive
-- consumer cannot race a ledger insertion by referencing a non-negative task.
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;BEGIN
 definition:=pg_get_functiondef('photo.v29_negative_consumer_allowed(uuid,uuid,uuid,uuid)'::regprocedure);
 needle:=' IF origin.operation<>''optical.negative'' OR origin.operation_version<>''1'' OR p_producer=p_consumer THEN RETURN true;END IF;';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'NEGATIVE_CONSUMER_LEDGER_PATCH_MISMATCH';END IF;
 definition:=replace(definition,needle,
 ' IF EXISTS(SELECT 1 FROM photo.v29_negative_invalidations WHERE tenant_id=p_tenant AND task_id=p_producer) THEN RETURN false;END IF;'
 ||E'\n'||needle);
 definition:=replace(definition,
 ' IF current_setting(''transaction_isolation'')<>''read committed'' THEN'||E'\n'||
 '  RAISE EXCEPTION ''NEGATIVE_CONSUMER_REQUIRES_READ_COMMITTED'' USING ERRCODE=''25001'';END IF;', '');
 IF strpos(definition,'NEGATIVE_CONSUMER_REQUIRES_READ_COMMITTED')>0 THEN RAISE EXCEPTION 'NEGATIVE_ISOLATION_PATCH_MISMATCH';END IF;
 EXECUTE definition;
 definition:=pg_get_functiondef('photo.guard_v29_negative_review_consumers()'::regprocedure);
 needle:='  PERFORM id FROM photo.tasks WHERE tenant_id=NEW.tenant_id AND id=NEW.task_id FOR UPDATE;';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'NEGATIVE_REVIEW_EPOCH_PATCH_MISMATCH';END IF;
 definition:=replace(definition,needle,'  UPDATE photo.tasks SET v29_dependency_epoch=v29_dependency_epoch+1 WHERE tenant_id=NEW.tenant_id AND id=NEW.task_id;');
 EXECUTE definition;
 definition:=pg_get_functiondef('photo.guard_v29_negative_content_consumer()'::regprocedure);
 IF strpos(definition,'origin.operation=''optical.negative'' AND origin.operation_version=''1''')=0 THEN
  RAISE EXCEPTION 'NEGATIVE_TRANSITIVE_CONSUMER_PATCH_MISMATCH';END IF;
 definition:=replace(definition,'origin.operation=''optical.negative'' AND origin.operation_version=''1''','true');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
