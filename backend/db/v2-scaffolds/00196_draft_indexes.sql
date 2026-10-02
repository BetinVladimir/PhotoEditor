-- One draft stream per source asset version; concurrent first PUT cannot fork history.
CREATE UNIQUE INDEX v2_draft_asset_version_once ON photo.v2_document_streams(tenant_id,asset_version_id)
 WHERE stream_type='draft';
