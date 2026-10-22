-- +goose Up
-- Preserve original manifest text in durable receipts; normalize only reads.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_derivative_receipt_document(p_receipt jsonb) RETURNS jsonb
 LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE manifest jsonb;
BEGIN
 IF jsonb_typeof(p_receipt) IS DISTINCT FROM 'object' OR octet_length(p_receipt::text)>2097152 THEN RETURN NULL;END IF;
 IF jsonb_typeof(p_receipt->'manifest')='string' THEN
  IF octet_length(p_receipt->>'manifest')>1048576 THEN RETURN NULL;END IF;
  manifest:=(p_receipt->>'manifest')::jsonb;
 ELSIF jsonb_typeof(p_receipt->'manifest')='object' THEN manifest:=p_receipt->'manifest';
 ELSE RETURN NULL;END IF;
 IF jsonb_typeof(manifest) IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 RETURN jsonb_set(p_receipt,'{manifest}',manifest,false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR program_limit_exceeded THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_derivative_receipt_document(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_derivative_receipt_document(jsonb) FROM PUBLIC;
-- Pure bounded projection with no table access, shared by API catalogue.
GRANT EXECUTE ON FUNCTION photo.v26_derivative_receipt_document(jsonb) TO photo_api;
-- +goose StatementBegin
DO $$ DECLARE signature text;definition text;needle text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.v28_open_eyes_review_preview_ready(uuid,uuid)',
  'photo.get_v28_open_eyes_review_selection(uuid,uuid,uuid,uuid)',
  'photo.v28_relight_review_preview_ready(uuid,uuid)',
  'photo.get_v28_relight_review_selection_pre_authority(uuid,uuid,uuid,uuid)'
 ] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  needle:='d.v26_derivative_receipt->''manifest''';
  IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'DERIVATIVE_MANIFEST_READ_PATCH_MISMATCH: %',signature;END IF;
  EXECUTE replace(definition,needle,'photo.v26_derivative_receipt_document(d.v26_derivative_receipt)->''manifest''');
 END LOOP;
 definition:=pg_get_functiondef('photo.v28_relight_viewed_pair_ready(uuid,uuid,uuid,uuid,bigint,jsonb)'::regprocedure);
 needle:='SELECT d.v26_derivative_receipt INTO receipt';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'VIEWED_MANIFEST_READ_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,'SELECT photo.v26_derivative_receipt_document(d.v26_derivative_receipt) INTO receipt');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
