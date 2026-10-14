"""Physical V28 source document read; synthetic metadata, no pixel qualification."""
import json
import os
from pathlib import Path
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import subprocess
import psycopg
import pytest
from psycopg.conninfo import make_conninfo
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from layer_original_fixture import seed_original_composition
from test_v27_native_protection_gc import lens_objects


@pytest.mark.parametrize("failure", ["", "corrupt", "missing-version"])
def test_local_portrait_reads_exact_source_version_with_current_authority(database,lens_objects,failure):
    client,bucket=lens_objects
    canvas=Canvas(width=5,height=5,orientation=1,space_id='oriented',source_sha256='a'*64)
    image=WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),canvas=canvas,color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='c'*64,encoding='gamma24'),dtype='float32',alpha=None,transform_chain=TransformChain.create([]))
    raw=canonical_bytes(image);digest=sha256(raw).hexdigest();key='local-source/'+uuid4().hex
    stored = raw if failure != 'corrupt' else b'x' * len(raw)
    version=client.put_object(bucket,key,BytesIO(stored),len(stored),content_type='application/json',metadata={'sha256':digest}).version_id
    # Latest key bytes differ; exact DB-pinned original version must be read.
    client.put_object(bucket,key,BytesIO(b'newer-invalid-document'),22,content_type='application/json')
    if failure == 'missing-version':
        client.remove_object(bucket,key,version_id=version)
    with psycopg.connect(**database) as db:
        case=seed_original_composition(db,source_object=dict(bucket=bucket,key=key,version=version,sha256=digest,bytes=len(raw)),canvas_hash=canonical_hash(canvas))
    cfg=client._photo_test_config;root=Path(__file__).resolve().parents[2]
    fixture={k:case[k] for k in ('Tenant','Project','Actor','Session')};fixture.update(Binding=json.loads(case['Params'])['base'],Source=raw.decode(),Failure=failure,Objects={**cfg,'Development':True})
    env={**os.environ,'PHOTO_TEST_DATABASE_URL':make_conninfo(**database),'PHOTO_TEST_LOCAL_SOURCE':json.dumps(fixture),'GOCACHE':'/private/tmp/photoeditor-go-cache'}
    result=subprocess.run(['go','test','./internal/v2draft','-run','^TestIntegrationLocalPortraitSourcePhysical$','-count=1','-timeout=30s','-v'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=45)
    assert result.returncode==0,result.stdout+result.stderr
    assert ('LOCAL_PORTRAIT_PINNED_SOURCE_REFUSED' if failure else 'LOCAL_PORTRAIT_PINNED_SOURCE_PHYSICAL_OK') in result.stdout
