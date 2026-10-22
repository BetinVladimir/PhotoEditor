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


def test_go_delivery_portal(database):
    """Use the existing disposable/migrated DB boundary for both complete packages.

    Ordinary package tests without PHOTO_TEST_DATABASE_URL skip their actual SQL
    cases; this invocation supplies the real isolated database rather than a
    predicate double. Object-store and browser paths remain separate fixtures.
    """
    root = Path(__file__).resolve().parents[2]
    env = dict(os.environ, PHOTO_TEST_DATABASE_URL=make_conninfo(**database),
               GOCACHE=str(root/'work/go-cache'))
    run = subprocess.run(['go', 'test', '-p', '1', './internal/delivery',
                          './internal/portal', '-count=1', '-v', '-timeout=300s'],
                         cwd=root/'backend', env=env, capture_output=True,
                         text=True, timeout=360)
    # Keep successful Go subtest/skip output too, not just a Python wrapper PASS.
    report = os.environ.get('PHOTO_TEST_GO_REPORT_DIR')
    if report:
        folder=Path(report).resolve()
        if not folder.is_relative_to(root):
            raise ValueError('Go integration report directory must be repository-local')
        folder.mkdir(parents=True,exist_ok=True)
        (folder/'go-delivery-portal-integration.log').write_text(run.stdout+run.stderr)
    assert run.returncode == 0, run.stdout + run.stderr
