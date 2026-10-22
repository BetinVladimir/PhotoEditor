import json
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.delivery_metadata_job import decode_job,metadata_adoption_installation,main
from photo_worker.v2.adoption import AdoptionInvalid
from io import BytesIO
from types import SimpleNamespace
import photo_worker.v2.delivery_metadata_job as metadata_job


def test_metadata_job_closed_canonical_identity():
    value=dict(schema_version=1,tenant_id=str(uuid4()),task_id=str(uuid4()),attempt=1,fence=2,result_id=str(uuid4()),timeout_seconds=60)
    scope,timeout=decode_job(canonical_bytes(value))
    assert str(scope.task_id)==value['task_id'] and timeout==60
    for mode in ('extra','bool','nil','fence','timeout','noncanonical'):
        changed=dict(value)
        if mode=='extra':changed['metadata_jcs']='{}'
        elif mode=='bool':changed['attempt']=True
        elif mode=='nil':changed['tenant_id']='00000000-0000-0000-0000-000000000000'
        elif mode=='fence':changed['fence']=2**53
        elif mode=='timeout':changed['timeout_seconds']=601
        raw=(json.dumps(changed,separators=(',',':'),sort_keys=True).encode() if mode=='fence' else canonical_bytes(changed))+(b' ' if mode=='noncanonical' else b'')
        with pytest.raises(ValueError):decode_job(raw)


def test_metadata_job_resource_admission(tmp_path):
    size=10000;pixels=1000
    values={'PHOTO_METADATA_ADOPTION_SCRATCH_ROOT':str(tmp_path),'PHOTO_METADATA_ADOPTION_MAX_BYTES':str(size),
        'PHOTO_METADATA_ADOPTION_MAX_PIXELS':str(pixels),'PHOTO_METADATA_ADOPTION_RAM_BYTES':str(256*1024**2+pixels*64+size*4),
        'PHOTO_METADATA_ADOPTION_SCRATCH_BYTES':str(size*2+8*1024**2)}
    installation=metadata_adoption_installation(values.get)
    assert installation.max_pixels==pixels
    for key in ('PHOTO_METADATA_ADOPTION_RAM_BYTES','PHOTO_METADATA_ADOPTION_SCRATCH_BYTES'):
        changed=dict(values);changed[key]=str(int(changed[key])-1)
        with pytest.raises(ValueError):metadata_adoption_installation(changed.get)
    changed=dict(values);changed['PHOTO_METADATA_ADOPTION_MAX_BYTES']=str(2*1024**3+1)
    with pytest.raises(ValueError):metadata_adoption_installation(changed.get)


def test_metadata_job_reports_only_closed_semantic_codes(monkeypatch):
    for error, expected in [
        (AdoptionInvalid('DELIVERY_METADATA_PHYSICAL_INVALID'),
         b'{"adopted":false,"error_code":"DELIVERY_METADATA_PHYSICAL_INVALID","schema_version":1}'),
        (OSError('disk unavailable'), b''),
        (AdoptionInvalid('secret detail'), b''),
    ]:
        output = BytesIO()
        monkeypatch.setattr(metadata_job, 'run_job', lambda _raw: (_ for _ in ()).throw(error))
        monkeypatch.setattr(metadata_job, 'sys', SimpleNamespace(
            stdin=SimpleNamespace(buffer=BytesIO(b'{}')),
            stdout=SimpleNamespace(buffer=output)))
        status = main()
        assert output.getvalue() == expected
        assert status == (0 if expected else 1)
