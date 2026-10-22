-- +goose Up
-- Historical adoption acknowledgement must still identify the exact canonical
-- attempt/fence, independently of artifact producer metadata.
-- +goose StatementBegin
DO $$ DECLARE source text;anchor text;replacement text; BEGIN
 source:=pg_get_functiondef('photo.v30_delivery_assembly_adoption_committed(uuid,uuid)'::regprocedure);
 anchor:=$old$SELECT outputs INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id FOR SHARE;$old$;
 replacement:=$new$SELECT outputs INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id
  AND attempt=t.attempt AND fencing_token=t.fencing_token FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;$new$;
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'delivery assembly receipt source drift';END IF;
 EXECUTE replace(source,anchor,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
