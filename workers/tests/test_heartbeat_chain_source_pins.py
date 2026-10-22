"""Wrapper pins originate in authored migrations, independently of live catalog."""
import hashlib
from pathlib import Path
import re
from photo_worker.runtime.database import TaskDatabase

def test_heartbeat_chain_pins_match_all_authored_wrapper_bodies():
    root=Path(__file__).resolve().parents[2]
    source=(root/'workers/photo_worker/runtime/database.py').read_text()
    probe=source.split('def task_result_fence_ready',1)[1].split('def removal_preview_runtime_ready',1)[0]
    wrappers=[]
    for migration in sorted((root/'backend/db/migrations').glob('*.sql')):
        match=re.search(r'CREATE FUNCTION photo\.heartbeat_task\(.*?AS \$\$(.*?)\$\$;',migration.read_text(),re.S)
        if match and migration.name!='00015_task_leases.sql':
            wrappers.append(hashlib.sha256(match.group(1).encode()).hexdigest())
    assert len(wrappers)==8
    assert all(probe.count(digest)==1 for digest in wrappers)
    assert 'count(*)=9' in probe
    assert "('heartbeat_task_pre_v23_series_device',NULL)" in probe
    assert "has_function_privilege(role.name,heartbeat.oid,'EXECUTE')" in probe
    assert "heartbeat.proargmodes=ARRAY['i','i','i','i','i','t','t','t']" in probe
    calls=[]
    database=TaskDatabase('unused')
    database._query=lambda query,args:(calls.append((query,args)) or (True,))
    assert database.task_result_fence_ready() is True
    assert len(calls)==1 and calls[0][1]==()


def test_native_variant_has_source_pinned_private_helper_bodies():
    import json
    root=Path(__file__).resolve().parents[2]
    fixtures=root/'tests/integration/fixtures'
    pins=json.loads((fixtures/'heartbeat-native-source-pins.json').read_text())
    sql=(fixtures/'heartbeat-native-helpers.sql').read_text()
    query=(root/'workers/photo_worker/runtime/database.py').read_text()
    assert len(pins)==2
    for pin in pins:
        match=re.search(r'CREATE OR REPLACE FUNCTION photo\.'+pin['function']+r'\(.*?AS (\$\w*\$)(.*?)\1\s*;',sql,re.S)
        assert match is not None
        assert hashlib.sha256(match.group(2).encode()).hexdigest()==pin['body_sha256']
        assert pin['body_sha256'] in query
    assert "expected.name='heartbeat_task'" in query
    assert 'ad68c2cc04acd441dafe096f07dd9404c7b1ed10e972a66c64c1b020d950597f' in query
    assert 'count(*)=2 AND bool_and(' in query
