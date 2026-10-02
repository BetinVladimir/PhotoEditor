-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.confirm_capture_frame(p_tenant uuid,p_receipt uuid,p_alert uuid,p_actor uuid,p_auth_session uuid)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt photo.capture_receipts%ROWTYPE; alert photo.capture_alerts%ROWTYPE;
 confirmation photo.capture_frame_confirmations%ROWTYPE; version uuid; person uuid; people uuid[]:='{}'; evidence jsonb;
BEGIN
 IF p_tenant IS NULL OR p_receipt IS NULL OR p_alert IS NULL OR p_actor IS NULL OR p_auth_session IS NULL
 OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN
  RAISE EXCEPTION 'invalid frame confirmation' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'frame confirmation forbidden' USING ERRCODE='42501'; END IF;
 SELECT * INTO receipt FROM photo.capture_receipts WHERE tenant_id=p_tenant AND id=p_receipt;
 IF NOT FOUND THEN RAISE EXCEPTION 'capture receipt missing' USING ERRCODE='P0002'; END IF;
 PERFORM id FROM photo.shoot_sessions WHERE tenant_id=p_tenant AND project_id=receipt.project_id AND id=receipt.session_id AND state<>'archived' FOR UPDATE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,receipt.project_id,p_actor,p_auth_session) THEN
  RAISE EXCEPTION 'frame confirmation forbidden' USING ERRCODE='42501';
 END IF;
 SELECT asset_version_id INTO version FROM photo.capture_receipt_assets WHERE tenant_id=p_tenant AND receipt_id=p_receipt;
 SELECT * INTO alert FROM photo.capture_alerts WHERE tenant_id=p_tenant AND project_id=receipt.project_id
 AND session_id=receipt.session_id AND asset_version_id=version AND id=p_alert FOR SHARE;
 IF NOT FOUND OR (alert.receipt_id IS NOT NULL AND alert.receipt_id IS DISTINCT FROM p_receipt) OR alert.quality_metric IS DISTINCT FROM 'capture.technical_quality/1'
 OR NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=alert.report_artifact_id
 AND a.sha256=alert.quality_report_sha AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'capture quality evidence unavailable' USING ERRCODE='23514';
 END IF;
 SELECT * INTO confirmation FROM photo.capture_frame_confirmations WHERE tenant_id=p_tenant AND receipt_id=p_receipt;
 IF FOUND THEN
  IF confirmation.alert_id<>p_alert THEN RAISE EXCEPTION 'frame confirmation conflict' USING ERRCODE='40001'; END IF;
  RETURN confirmation.id;
 END IF;
 FOR person IN SELECT DISTINCT person_id FROM photo.person_observations WHERE tenant_id=p_tenant
 AND project_id=receipt.project_id AND asset_version_id=version AND confirmed_by IS NOT NULL ORDER BY person_id LOOP
  PERFORM id FROM photo.project_persons WHERE tenant_id=p_tenant AND project_id=receipt.project_id AND id=person AND state='active' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
  PERFORM id FROM photo.consent_grants WHERE tenant_id=p_tenant AND project_id=receipt.project_id AND person_id=person AND purpose='identity_matching' AND status='granted' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
  people:=array_append(people,person);
 END LOOP;
 SELECT coalesce(jsonb_agg(jsonb_build_object('person_id',g.person_id,'consent_id',g.id,'epoch',g.epoch::text) ORDER BY g.person_id),'[]'::jsonb)
 INTO evidence FROM photo.consent_grants g WHERE g.tenant_id=p_tenant AND g.project_id=receipt.project_id
 AND g.purpose='identity_matching' AND g.status='granted' AND g.person_id=ANY(people);
 IF jsonb_array_length(evidence)>100 THEN RAISE EXCEPTION 'too many confirmed frame people' USING ERRCODE='22023'; END IF;
 INSERT INTO photo.capture_frame_confirmations(tenant_id,project_id,session_id,receipt_id,asset_version_id,alert_id,confirmed_by,identity_evidence)
 VALUES(p_tenant,receipt.project_id,receipt.session_id,p_receipt,version,p_alert,p_actor,evidence) RETURNING id INTO person;
 RETURN person;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.confirm_capture_frame(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.confirm_capture_frame(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.confirm_capture_frame(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
