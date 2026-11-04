#!/usr/bin/env python3
"""Builds a scratch Postgres database for the video SQL tests and runs them.

A database built from every repo migration fails at 02650 (its dispatch-body hashes belong to the owner's installed
database, see docs/video/PLAN.md), so this applies migrations <= 02649 and >= 02683 and skips 02650..02682.
Usage:  scripts/video/scratch-db.py --socket-dir DIR --port 55433 --user photo_test --db scratch_v [--run]
The cluster must already run (initdb + pg_ctl -o "-p PORT -k DIR"). --run then executes tests/integration/test_video_*.py
through a minimal conftest that supplies the `db` fixture (rolled back after every test).
"""
import argparse
import os
import pathlib
import subprocess
import sys
import tempfile

import psycopg

ROOT = pathlib.Path(__file__).resolve().parents[2]
SKIP = range(2650, 2683)

CONFTEST = '''import os, sys, pytest, psycopg
sys.path.insert(0, {tests!r})
sys.path.insert(0, {workers!r})
KW = dict(host={host!r}, port={port}, user={user!r}, dbname={db!r})
@pytest.fixture
def db():
    with psycopg.connect(**KW) as conn:
        with conn.transaction(force_rollback=True):
            yield conn
'''


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument('--socket-dir', required=True)
    ap.add_argument('--port', type=int, default=55433)
    ap.add_argument('--user', default='photo_test')
    ap.add_argument('--db', default='scratch_v')
    ap.add_argument('--run', action='store_true')
    a = ap.parse_args()
    kw = dict(host=a.socket_dir, port=a.port, user=a.user)
    files = sorted(p for p in (ROOT / 'backend/db/migrations').glob('*.sql') if int(p.name[:5]) not in SKIP)
    with psycopg.connect(**kw, dbname='postgres', autocommit=True) as c:
        c.execute(f'DROP DATABASE IF EXISTS {a.db} WITH (FORCE)')
        c.execute(f'CREATE DATABASE {a.db}')
    with psycopg.connect(**kw, dbname=a.db, autocommit=True) as c:
        for p in files:
            up = p.read_text().split('-- +goose Down')[0]
            try:
                if '-- +goose NO TRANSACTION' in up:
                    c.execute(up)
                else:
                    with c.transaction():
                        c.execute(up)
            except Exception as e:  # noqa: BLE001
                print('FAIL', p.name, str(e)[:300])
                return 1
    print('built', len(files), 'migrations into', a.db)
    if not a.run:
        return 0
    with tempfile.TemporaryDirectory() as d:
        pathlib.Path(d, 'video_scratch_fixtures.py').write_text(CONFTEST.format(
            tests=str(ROOT / 'tests/integration'), workers=str(ROOT / 'workers'), host=a.socket_dir, port=a.port, user=a.user, db=a.db))
        env = dict(os.environ, PHOTO_TEST_NATIVE_INFRA='1', PYTHONPATH=d)
        tests = [str(t) for t in sorted((ROOT / 'tests/integration').glob('test_video_*.py'))]
        return subprocess.call([sys.executable, '-m', 'pytest', '-q', '--noconftest', '-p', 'video_scratch_fixtures', '-p', 'no:cacheprovider', *tests], env=env)


if __name__ == '__main__':
    sys.exit(main())
