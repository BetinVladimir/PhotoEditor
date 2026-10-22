"""Installed SQL catalog shape; no dispatch or model qualification proof."""
import pytest
from test_v27_removal_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('operation,resource,route,model,backends,roles',[
    ('semantic.preview_background_cleanup','gpu','gpu.semantic_preview_remove_object','big_lama',
        ['cpu','cuda','mps'],{'preview_evidence','working_image','pixels','preview_presentation','before','after','affected','protected'}),
    ('semantic.adopt_shown_background_cleanup','cpu','cpu.semantic_adopt_shown_background_cleanup',None,
        ['cpu'],{'working_image','pixels'}),
])
def test_background_cleanup_catalog_matches_own_worker_contract(database,operation,resource,route,model,backends,roles):
    row=database.execute('SELECT resource_class,routing_key,model_id,timeout_seconds,backends '
        'FROM photo.operation_registry WHERE operation=%s AND version=%s',(operation,'1')).fetchone()
    assert row==(resource,route,model,1800,backends)
    policies=database.execute('SELECT role,media_types,max_bytes,max_count FROM photo.operation_output_policies '
        'WHERE operation=%s AND version=%s',(operation,'1')).fetchall()
    assert len(policies)==len(roles)
    assert {item[0] for item in policies}==roles
    for role,media_types,max_bytes,max_count in policies:
        expected_media='application/x-npy' if role=='pixels' else 'image/png' if role in {'before','after','affected','protected'} else 'application/json'
        assert media_types==[expected_media]
        assert max_count==1 and max_bytes>0
    # Queue routing may share a suffix with removal. The command's operation,
    # not the suffix, selects the executor; it must remain a distinct SQL row.
    assert database.execute('SELECT count(*) FROM photo.operation_registry WHERE operation=%s AND version=%s',
        (operation,'1')).fetchone()==(1,)
