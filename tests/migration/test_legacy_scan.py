import hashlib
import json
from pathlib import Path
import pytest
from photo_worker.legacy_migrate import MigrationError, execute, main, scan, validate

def write(path: Path, data: bytes):
    path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(data); return hashlib.sha256(data).hexdigest()

def fixture(root: Path):
    source=write(root/'input'/'portrait.jpg',b'original bytes')
    output=write(root/'output'/'runs'/'run-1'/'delivery'/'portrait.jpg',b'edited bytes')
    run=root/'output'/'runs'/'run-1'
    (run/'manifest.json').write_text(json.dumps({'input_root':str(root/'input'),'items':[{'relative':'portrait.jpg','sha256':source,'source_id':'one'}]}))
    (run/'report.json').write_text(json.dumps({'images':[{'source_id':'one','outputs':{'delivery/portrait.jpg':output}}]}))
    (run/'resolved-config.json').write_text(json.dumps({'schema_version':1,'profile':'legacy'}))
    return source,output

def test_scan_is_deterministic_read_only_and_revalidates_bytes(tmp_path):
    fixture(tmp_path); before={p.relative_to(tmp_path):p.read_bytes() for p in tmp_path.rglob('*') if p.is_file()}
    first,second=scan(tmp_path),scan(tmp_path)
    assert first.valid and first.plan_hash==second.plan_hash and len(first.files)==2
    assert before=={p.relative_to(tmp_path):p.read_bytes() for p in tmp_path.rglob('*') if p.is_file()}
    (tmp_path/'input'/'portrait.jpg').write_bytes(b'changed')
    assert any(issue.code=='CORRUPT' for issue in validate(first))

def test_scan_reports_missing_corrupt_and_unsupported_config(tmp_path):
    fixture(tmp_path); run=tmp_path/'output'/'runs'/'run-1'
    (run/'resolved-config.json').write_text(json.dumps({'schema_version':99}))
    (run/'delivery'/'portrait.jpg').write_bytes(b'corrupt')
    plan=scan(tmp_path)
    assert not plan.valid and {x.code for x in plan.issues}>={'CORRUPT','UNSUPPORTED_CONFIG'}

def test_scan_rejects_traversal_duplicate_keys_and_symlinks(tmp_path):
    fixture(tmp_path); run=tmp_path/'output'/'runs'/'run-1'
    (run/'manifest.json').write_text('{"input_root":"x","input_root":"y","items":[]}')
    assert any(issue.code=='UNSAFE' for issue in scan(tmp_path).issues)
    if hasattr(Path,'symlink_to'):
        root=tmp_path/'linked'; root.symlink_to(tmp_path,target_is_directory=True)
        with pytest.raises(MigrationError): scan(root)

def test_cli_writes_reviewable_plan_only_inside_selected_root(tmp_path):
    fixture(tmp_path)
    output=tmp_path/'plans'/'legacy.json'
    assert main([str(tmp_path),'--output',str(output)])==0
    saved=json.loads(output.read_text())
    assert saved['valid'] and saved['plan_hash']==scan(tmp_path).plan_hash
    assert main([str(tmp_path),'--output',str(tmp_path.parent/'escape.json')])==2
    assert not (tmp_path.parent/'escape.json').exists()

class Transport:
    def __init__(self):self.calls=[];self.parts=[];self.collections=0
    def json(self,method,path,body=None,*,idempotency_key=None):
        self.calls.append((method,path,body,idempotency_key))
        if path.endswith('/collections'):
            self.collections+=1
            return {'collection':{'id':f'40000000-0000-0000-0000-{self.collections:012d}'}}
        if method=='GET' and '/uploads/' in path:
            return {'id':path.rsplit('/',1)[1],'state':'verified'}
        if path.endswith('/legacy-imports/reconcile'):
            return {'id':'60000000-0000-0000-0000-000000000001','asset_version_id':'70000000-0000-0000-0000-000000000001',**body}
        if path.endswith('/parts'):
            return {'upload_id':'10000000-0000-0000-0000-000000000001','items':[{'part_number':n,'url':f'https://upload.test/{n}'} for n in body['part_numbers']]}
        if path.endswith('/complete'):return {'job_id':'10000000-0000-0000-0000-000000000001','status':'queued'}
        return {'id':'10000000-0000-0000-0000-000000000001','state':'uploading','part_bytes':7,'parts_total':2}
    def put(self,url,body):self.parts.append((url,body));return 'etag-'+url.rsplit('/',1)[1]

def test_execute_revalidates_and_uses_ordinary_multipart_api(tmp_path):
    fixture(tmp_path);plan=scan(tmp_path);transport=Transport();tenant='20000000-0000-0000-0000-000000000001';project='30000000-0000-0000-0000-000000000001'
    result=execute(plan,transport,tenant,project)
    assert len(result)==2 and all(x['asset_version_id'].startswith('70000000-') for x in result)
    assert transport.collections==2
    begin=[call for call in transport.calls if call[1].endswith('/uploads')]
    assert len(begin)==2 and len({call[2]['collection_id'] for call in begin})==2
    assert b''.join(body for _,body in transport.parts[:2])==b'edited bytes'
    reconciles=[call for call in transport.calls if call[1].endswith('/legacy-imports/reconcile')]
    assert len(reconciles)==2 and {call[2]['source_relative'] for call in reconciles}=={item.relative for item in plan.files}
    keys=[call[3] for call in transport.calls if call[3] is not None]
    assert len(keys)==6 and len(set(keys))==6 and all(key.startswith('legacy-') for key in keys)

def test_execute_uses_explicit_source_collection_but_still_separates_runs(tmp_path):
    fixture(tmp_path);plan=scan(tmp_path);transport=Transport()
    source='50000000-0000-0000-0000-000000000001'
    execute(plan,transport,'20000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000001',source)
    assert transport.collections==1
    begin=[call for call in transport.calls if call[1].endswith('/uploads')]
    source_begin=next(call for call in begin if call[2]['filename']=='portrait.jpg' and call[2]['sha256']==plan.files[1].sha256)
    assert source_begin[2]['collection_id']==source

def test_execute_stops_before_api_when_file_changed(tmp_path):
    fixture(tmp_path);plan=scan(tmp_path);(tmp_path/'input'/'portrait.jpg').write_bytes(b'changed')
    transport=Transport()
    with pytest.raises(MigrationError):execute(plan,transport,'20000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000001')
    assert transport.calls==[]

@pytest.mark.parametrize('field,value',[
    ('upload_id','10000000-0000-0000-0000-000000000002'),
    ('asset_version_id','00000000-0000-0000-0000-000000000000'),
    ('plan_hash','0'*64),('source_relative','other.jpg'),
    ('source_sha256','0'*64),('provenance',[]),('unexpected',True),
])
def test_execute_rejects_malformed_or_foreign_reconciliation_receipt(tmp_path,field,value):
    fixture(tmp_path);plan=scan(tmp_path)
    class BrokenReceipt(Transport):
        def json(self,method,path,body=None,*,idempotency_key=None):
            result=super().json(method,path,body,idempotency_key=idempotency_key)
            if path.endswith('/legacy-imports/reconcile'):result[field]=value
            return result
    transport=BrokenReceipt()
    with pytest.raises(MigrationError):
        execute(plan,transport,'20000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000001')
    assert len([c for c in transport.calls if c[1].endswith('/legacy-imports/reconcile')])==1

def test_cli_requires_explicit_execute_flag_for_scope(tmp_path, capsys):
    fixture(tmp_path)
    assert main([str(tmp_path),'--tenant-id','20000000-0000-0000-0000-000000000001'])==2
    failure=json.loads(capsys.readouterr().out.splitlines()[-1])
    assert failure['code']=='LEGACY_SCAN_FAILED'
    assert 'require --execute-api' in failure['detail']

def test_cli_requires_token_and_ids_before_constructing_transport(tmp_path, monkeypatch, capsys):
    fixture(tmp_path)
    monkeypatch.delenv('PHOTOEDITOR_TOKEN',raising=False)
    assert main([str(tmp_path),'--execute-api','https://api.example.test'])==2
    failure=json.loads(capsys.readouterr().out.splitlines()[-1])
    assert failure['code']=='LEGACY_SCAN_FAILED'
    assert 'PHOTOEDITOR_TOKEN' in failure['detail']
