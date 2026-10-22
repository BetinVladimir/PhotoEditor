"""Static regression: retain every earlier shown-copy lifecycle gate."""
import re
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]/'backend/db/migrations'


def function_body(filename,name):
    source=(ROOT/filename).read_text()
    match=re.search(r'CREATE OR REPLACE FUNCTION photo\.'+re.escape(name)+r'\(.*?AS \$\$(.*?)\$\$;',source,re.S)
    assert match is not None
    return match.group(1)


def without_retouch(body):
    body=body.replace(",'semantic.adopt_shown_retouch_region'",'')
    body=body.replace("\n    OR v.id=photo.semantic_uuid_v5(photo.semantic_uuid_v5(source.id,'semantic.adopt_shown_retouch_region:'||id::text),'V27.ShownAppliedVersion')",'')
    return body


def test_retouch_lifecycle_preserves_all_previous_function_bodies():
    for name,old,new in [
        ('finalize_shown_semantic_apply_child','02336_v27_shown_background_cleanup_child_finalization.sql','02387_v27_shown_retouch_child_projection.sql'),
        ('finalize_next_shown_semantic_apply_child','02336_v27_shown_background_cleanup_child_finalization.sql','02387_v27_shown_retouch_child_projection.sql'),
        ('semantic_shown_applied_image','02337_v27_shown_background_cleanup_applied_image.sql','02387_v27_shown_retouch_child_projection.sql'),
        ('register_media_retry_node','02338_v27_shown_background_cleanup_media_sources.sql','02388_v27_shown_retouch_media_sources.sql'),
        ('semantic_shown_media_source_live','02338_v27_shown_background_cleanup_media_sources.sql','02388_v27_shown_retouch_media_sources.sql'),
    ]:
        previous=function_body(old,name)
        current=function_body(new,name)
        assert without_retouch(current)==previous
        if name!='finalize_next_shown_semantic_apply_child':
            assert 'semantic.adopt_shown_retouch_region' in current


def test_retouch_media_remains_exact_copy_and_lineage_bound():
    for name in ('register_media_retry_node','semantic_shown_media_source_live'):
        body=function_body('02388_v27_shown_retouch_media_sources.sql',name)
        assert "lineage.role='semantic_source' AND lineage.ordinal=0" in body
        assert "v.review_state<>'accepted'" in body
        assert "source.status<>'completed'" in body
        assert "'V27.ShownAppliedVersion'" in body
        assert "i.consumer='v2-adopt'" in body
        assert "z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',v.master_artifact_id,'sha256',v.content_hash))" in body
        assert "v.metadata->'pixels'" in body and "v.metadata->'alpha'" in body
    assert 'RETURN photo.register_media_retry_node_pre_shown_source(' in function_body(
        '02388_v27_shown_retouch_media_sources.sql','register_media_retry_node')
