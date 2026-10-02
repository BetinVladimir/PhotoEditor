"""Exercise the real source-pinned MinIO development service through Go SDK."""
from pathlib import Path
import os
import subprocess
import pytest


def test_go_minio():
    root=Path(__file__).resolve().parents[2]
    config=root/'work/minio.env'
    if not config.exists():pytest.skip('Build and start the dedicated development MinIO first')
    result=subprocess.run(['docker','inspect','--format','{{index .Config.Labels "photoeditor.scope"}}','photoeditor-dev-minio'],capture_output=True,text=True,timeout=10)
    assert result.returncode==0 and result.stdout.strip()=='development','Dedicated MinIO unavailable'
    values=dict(line.split('=',1) for line in config.read_text().splitlines() if '=' in line)
    env=dict(os.environ,PHOTO_TEST_MINIO_URL='http://127.0.0.1:59000',PHOTO_TEST_MINIO_ACCESS=values['MINIO_ROOT_USER'],PHOTO_TEST_MINIO_SECRET=values['MINIO_ROOT_PASSWORD'],GOCACHE=str(root/'work/go-cache'))
    result=subprocess.run(['go','test','./internal/objectstore','-run','Integration','-count=1','-timeout=120s'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=150)
    assert result.returncode==0,result.stdout+result.stderr


def test_go_upload_completion(database):
    from psycopg.conninfo import make_conninfo
    root=Path(__file__).resolve().parents[2]
    config=root/'work/minio.env'
    if not config.exists():pytest.skip('Build and start the dedicated development MinIO first')
    result=subprocess.run(['docker','inspect','--format','{{index .Config.Labels "photoeditor.scope"}}','photoeditor-dev-minio'],capture_output=True,text=True,timeout=10)
    assert result.returncode==0 and result.stdout.strip()=='development','Dedicated MinIO unavailable'
    values=dict(line.split('=',1) for line in config.read_text().splitlines() if '=' in line)
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_MINIO_URL='http://127.0.0.1:59000',PHOTO_TEST_MINIO_ACCESS=values['MINIO_ROOT_USER'],PHOTO_TEST_MINIO_SECRET=values['MINIO_ROOT_PASSWORD'],GOCACHE=str(root/'work/go-cache'))
    result=subprocess.run(['go','test','./internal/upload','-run','IntegrationStorage','-count=1','-timeout=120s'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=150)
    assert result.returncode==0,result.stdout+result.stderr
