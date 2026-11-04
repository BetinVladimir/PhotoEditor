#!/usr/bin/env python3
"""Rebuilds a database with the *installed* migration chain, ending at the repo head.

The repo cannot build a database from zero (see docs/video/PLAN.md): the owner's installed history is
  repo 00001-02597  +  native 02598-02625 (only inside the snapshot)  +  billing as 02626-02637 (repo: 02598-02609)
  +  native 02638-02640 (work/ source views)  +  repo 02641 and later.
So the chain is: restore the accepted logical snapshot of the installed database (head 2637), apply native
02638-02640, then every repo migration from 02641 on.

Two things are patched in temporary copies only (never in the repo):
  * 02639/02640 compare pg_proc metadata hashes that include the plpgsql language OID, which differs between
    PostgreSQL builds (owner: Postgres.app 18.4); the function-body hashes stay checked;
  * the cluster superuser is named rtx_snapshot_admin because the same migrations compare the full role list.

Usage: scripts/video/rebuild-owner-chain.py --cluster DIR --socket-dir /tmp/ownerpg --port 55434 \
         [--snapshot work/rtx-photographer-journey-20261021/qualified-snapshot-export-r3] \
         [--native work/scoped-native2639-rollout-r1/migrations work/gateway-output-alias-goose-r1/migrations]
PostgreSQL 18 binaries are taken from --pg-bin (default /opt/homebrew/opt/postgresql@18/bin).
"""
import argparse
import os
import pathlib
import re
import subprocess
import sys
import tempfile

import psycopg

ROOT = pathlib.Path(__file__).resolve().parents[2]
SUPER = 'rtx_snapshot_admin'
DB = 'photo_capture_13b4b4b4c0034608b621374b5435643b'
EXTRA_ROLES = ['rtx_cpu_9961e24a95e549b9', 'rtx_media_26cddb721108458b', 'rtx_test_api', 'rtx_test_auth', 'rtx_test_collection',
               'rtx_test_gateway', 'rtx_test_mail', 'rtx_test_maintenance', 'rtx_test_publisher', 'rtx_test_scheduler', 'rtx_test_worker']
HASH_GUARD = 'OR actual_hash IS DISTINCT FROM target.metadata_sha '


def sh(*a, **k):
    subprocess.run(a, check=True, **k)


def apply(conn, path: pathlib.Path, text: str) -> None:
    n = int(re.match(r'(\d+)_', path.name).group(1))
    up = text.split('-- +goose Down')[0]
    if '-- +goose NO TRANSACTION' in up:
        conn.execute(up)
    else:
        with conn.transaction():
            conn.execute(up)
    conn.execute('INSERT INTO public.goose_db_version(version_id,is_applied) VALUES (%s,true)', (n,))
    print('OK', path.name, flush=True)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument('--cluster', required=True)
    ap.add_argument('--socket-dir', default='/tmp/ownerpg')
    ap.add_argument('--port', type=int, default=55434)
    ap.add_argument('--pg-bin', default='/opt/homebrew/opt/postgresql@18/bin')
    ap.add_argument('--snapshot', default=str(ROOT / 'work/rtx-photographer-journey-20261021/qualified-snapshot-export-r3'))
    ap.add_argument('--native', nargs='*', default=[str(ROOT / 'work/scoped-native2639-rollout-r1/migrations'),
                                                   str(ROOT / 'work/gateway-output-alias-goose-r1/migrations')])
    a = ap.parse_args()
    b, cluster, snap = a.pg_bin, pathlib.Path(a.cluster), pathlib.Path(a.snapshot)
    os.makedirs(a.socket_dir, exist_ok=True)
    if not (cluster / 'data').exists():
        sh(f'{b}/initdb', '-D', str(cluster / 'data'), '-U', SUPER, '--auth=trust', '-E', 'UTF8', stdout=subprocess.DEVNULL)
    sh(f'{b}/pg_ctl', '-D', str(cluster / 'data'), '-o', f'-p {a.port} -k {a.socket_dir} -c listen_addresses=',
       '-l', str(cluster / 'pg.log'), '-w', 'start', stdout=subprocess.DEVNULL)
    kw = dict(host=a.socket_dir, port=a.port, user=SUPER)
    env = dict(os.environ, PGHOST=a.socket_dir, PGPORT=str(a.port), PGUSER=SUPER)
    sh(f'{b}/psql', '-d', 'postgres', '-q', '-f', str(snap / 'globals.sql'), env=env)
    with psycopg.connect(**kw, dbname='postgres', autocommit=True) as c:
        for r in EXTRA_ROLES:
            c.execute(f'CREATE ROLE {r} NOLOGIN')
        c.execute(f'DROP DATABASE IF EXISTS {DB} WITH (FORCE)')
        c.execute(f'CREATE DATABASE {DB}')
    sh(f'{b}/pg_restore', '-d', DB, str(snap / 'database.dump'), env=env)
    natives = sorted((p for d in a.native for p in pathlib.Path(d).glob('02*.sql') if re.match(r'026(3[89]|40)_', p.name)
                      and 'rehearsal' not in p.name), key=lambda p: p.name)
    repo = sorted(p for p in (ROOT / 'backend/db/migrations').glob('*.sql') if int(p.name[:5]) >= 2641)
    with psycopg.connect(**kw, dbname=DB, autocommit=True) as c:
        for p in natives:
            apply(c, p, p.read_text().replace(HASH_GUARD, ''))
        for p in repo:
            apply(c, p, p.read_text())
        print('head', c.execute('SELECT max(version_id) FROM public.goose_db_version').fetchone()[0])
    return 0


if __name__ == '__main__':
    sys.exit(main())
