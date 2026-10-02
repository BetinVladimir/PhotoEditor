"""Run Go integration tests against a disposable, migrated database."""
import os
from pathlib import Path
import subprocess
from psycopg.conninfo import make_conninfo

def test_go_auth(database):
    root = Path(__file__).resolve().parents[2]
    env = dict(os.environ, PHOTO_TEST_DATABASE_URL=make_conninfo(**database),
               GOCACHE=str(root/'work/go-cache'))
    run = subprocess.run(['go', 'test', './internal/auth', './internal/tenant', './internal/httpapi', './internal/mail', './internal/workflow', './internal/v2workflow', './internal/v2draft', './internal/v2sessions', './internal/legacyimport', './internal/upload', './internal/download', './internal/style', '-run', 'Integration', '-count=1', '-timeout=60s'],
                         cwd=root/'backend', env=env, capture_output=True, text=True, timeout=90)
    assert run.returncode == 0, run.stdout + run.stderr


def test_go_queue(database, rabbit_vhost):
    root = Path(__file__).resolve().parents[2]
    env = dict(os.environ, PHOTO_TEST_DATABASE_URL=make_conninfo(**database),
               PHOTO_TEST_AMQP_URL=rabbit_vhost, GOCACHE=str(root/'work/go-cache'))
    run = subprocess.run(['go', 'test', './internal/queue', './internal/collection', '-run', 'Integration', '-count=1', '-timeout=60s'],
                         cwd=root/'backend', env=env, capture_output=True, text=True, timeout=90)
    assert run.returncode == 0, run.stdout + run.stderr
