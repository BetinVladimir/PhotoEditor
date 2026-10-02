"""Opt-in actual >4GiB Store.PutImmutable and verified pinned read on MinIO."""
from pathlib import Path
import os
import shutil
import subprocess
import pytest
from test_v25_lens_objectstore import lens_objects

ROOT=Path(__file__).resolve().parents[2]


@pytest.mark.skipif(os.environ.get('PHOTO_RUN_S3_LARGE_IO')!='1',reason='Opt-in >4GiB S3 IO')
def test_real_large_immutable_s3_io(lens_objects):
    if os.environ.get('PHOTO_RUN_S3_LARGE_IO')!='1':pytest.skip('Set PHOTO_RUN_S3_LARGE_IO=1')
    if shutil.disk_usage(ROOT).free<8*2**30:pytest.fail('8GiB free required')
    client,_=lens_objects
    credentials=client._provider.retrieve()
    env=dict(os.environ,PHOTO_RUN_S3_LARGE_IO='1',PHOTO_TEST_MINIO_URL='http://'+client._base_url.host,
        PHOTO_TEST_MINIO_ACCESS=credentials.access_key,PHOTO_TEST_MINIO_SECRET=credentials.secret_key,
        GOCACHE=str(ROOT/'.runtime/go-cache'))
    process=subprocess.run(['go','test','./internal/objectstore','-run','^TestIntegrationLargeImmutableIO$',
        '-count=1','-timeout=330s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=340)
    assert process.returncode==0,process.stdout+process.stderr
    print(process.stdout,flush=True)
