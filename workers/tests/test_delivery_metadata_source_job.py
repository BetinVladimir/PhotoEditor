from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import json
import sys
import pytest
from PIL import Image
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.delivery_metadata_source_job import decode_job,prepare_job,source_installation
from photo_worker.v2.adoption import AdoptionInvalid


def job_value(body):
    return {'schema_version':1,'scope':{'tenant_id':str(uuid4()),'project_id':str(uuid4()),'actor_id':str(uuid4()),'session_id':str(uuid4()),
        'binding':{'asset_version_id':str(uuid4()),'source':{'artifact_id':str(uuid4()),'sha256':'b'*64},'canvas_hash':'c'*64},
        'original':{'artifact_id':str(uuid4()),'sha256':sha256(body).hexdigest()},'original_bytes':len(body),'original_mime':'image/jpeg'},
        'metadata_id':str(uuid4()),'policy':'copyright','approved_fields':[],'timeout_seconds':60}


def test_source_job_actual_original_preparation_outside_sql(tmp_path):
    stream=BytesIO();image=Image.new('RGB',(4,3));exif=Image.Exif();exif[315]='Artist';image.save(stream,format='JPEG',exif=exif)
    body=stream.getvalue();value=job_value(body);job,scope=decode_job(canonical_bytes(value))
    class Connection:
        autocommit=True;inside=False
        @contextmanager
        def transaction(self):
            assert not self.inside
            self.inside=True
            try:yield
            finally:self.inside=False
        def execute(self,query,args=()):
            assert self.inside
            row=(scope.original.artifact_id,scope.original.sha256,len(body),'photos','original','v1','image/jpeg')
            return SimpleNamespace(fetchone=lambda:row)
    connection=Connection()
    class Objects:
        calls=0
        def get_object(self,bucket,key,*,version_id):
            assert not connection.inside and (bucket,key,version_id)==('photos','original','v1')
            self.calls+=1;return BytesIO(body)
    objects=Objects()
    result=prepare_job(connection,objects,job,scope,bucket='photos',scratch_root=tmp_path,max_bytes=100000,check_active=lambda:None)
    raw=result['publication_jcs'].encode()
    assert result['publication_sha256']==sha256(raw).hexdigest()
    envelope=json.loads(raw)
    assert json.loads(envelope['metadata_jcs'])['author']=='Artist'
    assert objects.calls==1 and not list(tmp_path.iterdir())
    job.scope.original_bytes+=1
    with pytest.raises(AdoptionInvalid):prepare_job(connection,objects,job,scope,bucket='photos',scratch_root=tmp_path,max_bytes=100000,check_active=lambda:None)
    assert objects.calls==1


def test_source_job_closed_approval_and_installation(tmp_path):
    value=job_value(b'x')
    for mode in ('extra','alias','approval','policy','canonical'):
        changed=json.loads(json.dumps(value))
        if mode=='extra':changed['metadata_jcs']='{}'
        elif mode=='alias':changed['metadata_id']=changed['scope']['original']['artifact_id']
        elif mode=='approval':changed['approved_fields']=['gps']
        elif mode=='policy':changed['policy']='strip'
        raw=canonical_bytes(changed)+(b' ' if mode=='canonical' else b'')
        with pytest.raises(ValueError):decode_job(raw)
    env={'PHOTO_METADATA_SOURCE_SCRATCH_ROOT':str(tmp_path),'PHOTO_METADATA_SOURCE_MAX_BYTES':'10000',
        'PHOTO_METADATA_SOURCE_RAM_BYTES':str(256*1024**2+40000),'PHOTO_METADATA_SOURCE_SCRATCH_BYTES':str(8*1024**2+10000)}
    assert source_installation(env.get)==(tmp_path,10000)
    env['PHOTO_METADATA_SOURCE_RAM_BYTES']='1'
    with pytest.raises(ValueError):source_installation(env.get)


def test_source_job_raw_requires_tool_and_keeps_sql_outside_object_io(tmp_path):
    body=b'raw fixture bytes';value=job_value(body)
    value['scope']['original_mime']='image/x-adobe-dng'
    job,scope=decode_job(canonical_bytes(value))
    executable=tmp_path/'exiftool';executable.write_text('#!'+sys.executable+'\n'
        'import sys\nsys.stdout.write(\'[{"File:FileType":"DNG","IFD0:Artist":"RAW Artist"}]\')\n')
    executable.chmod(0o700)
    class Connection:
        autocommit=True;inside=False
        @contextmanager
        def transaction(self):
            self.inside=True
            try:yield
            finally:self.inside=False
        def execute(self,query,args=()):
            assert self.inside
            row=(scope.original.artifact_id,scope.original.sha256,len(body),'photos',
                 'original','v1','image/x-adobe-dng')
            return SimpleNamespace(fetchone=lambda:row)
    connection=Connection()
    class Objects:
        def get_object(self,bucket,key,*,version_id):
            assert not connection.inside
            return BytesIO(body)
    with pytest.raises(AdoptionInvalid):
        prepare_job(connection,Objects(),job,scope,bucket='photos',scratch_root=tmp_path,
                    max_bytes=100000,check_active=lambda:None)
    result=prepare_job(connection,Objects(),job,scope,bucket='photos',scratch_root=tmp_path,
                       max_bytes=100000,raw_executable=str(executable),
                       raw_tool_sha=sha256(executable.read_bytes()).hexdigest(),check_active=lambda:None)
    envelope=json.loads(result['publication_jcs'])
    assert json.loads(envelope['metadata_jcs'])['author']=='RAW Artist'
