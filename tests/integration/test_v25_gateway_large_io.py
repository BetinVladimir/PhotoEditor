"""Opt-in real S3 transfer via production gateway handler with synthetic authority."""
from pathlib import Path
import os
import shutil
import subprocess
import pytest
from test_v25_lens_objectstore import lens_objects

ROOT=Path(__file__).resolve().parents[2]


@pytest.mark.skipif(os.environ.get('PHOTO_RUN_GATEWAY_LARGE_IO')!='1',reason='Opt-in >4GiB gateway IO')
def test_real_large_output_handler(lens_objects):
    if shutil.disk_usage(ROOT).free<8*2**30:pytest.fail('8GiB free required')
    client,_=lens_objects
    credentials=client._provider.retrieve()
    env=dict(os.environ,PHOTO_RUN_GATEWAY_LARGE_IO='1',PHOTO_TEST_MINIO_URL='http://'+client._base_url.host,
        PHOTO_TEST_MINIO_ACCESS=credentials.access_key,PHOTO_TEST_MINIO_SECRET=credentials.secret_key,
        GOCACHE=str(ROOT/'.runtime/go-cache'))
    result=subprocess.run(['go','test','./internal/gateway','-run','^TestIntegrationLargeOutputHandler$',
        '-count=1','-timeout=330s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=340)
    assert result.returncode==0,result.stdout+result.stderr
    print(result.stdout,flush=True)
