"""Real Go presigning and HTTP download against an owned disposable MinIO."""
import os
import subprocess
from pathlib import Path

from test_v25_lens_objectstore import lens_objects


def test_signed_download_pins_version(lens_objects):
    client, _ = lens_objects
    config = client._photo_test_config
    env = dict(os.environ, PHOTO_TEST_MINIO_URL=config['Endpoint'],
               PHOTO_TEST_MINIO_ACCESS=config['AccessKey'],
               PHOTO_TEST_MINIO_SECRET=config['SecretKey'],
               GOCACHE='/private/tmp/photoeditor-go-cache')
    result = subprocess.run(
        ['go', 'test', './internal/objectstore', '-run',
         '^TestIntegrationSignedDownloadPinsVersion$', '-count=1', '-v'],
        cwd=Path(__file__).resolve().parents[2] / 'backend', env=env,
        capture_output=True, text=True, timeout=90)
    assert result.returncode == 0, result.stdout + result.stderr
    assert '--- PASS: TestIntegrationSignedDownloadPinsVersion' in result.stdout
