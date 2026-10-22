"""Export identities of a real completed/current removal preview for Go HTTP tests.

Read-only DB transaction; never creates preview authority or synthetic objects.
Run only in the later integration phase against an isolated migrated test DB.
"""
import argparse
import json
import os
import re
from pathlib import Path
from uuid import UUID

import psycopg


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run', required=True, type=UUID)
    parser.add_argument('--icc-lock-sha', required=True)
    parser.add_argument('--srgb-sha', required=True)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    output = args.output.resolve()
    if not output.is_relative_to(root):
        parser.error('output must remain inside PhotoEditor')
    for value in (args.icc_lock_sha, args.srgb_sha):
        if not re.fullmatch(r'[0-9a-f]{64}', value):
            parser.error('profile hashes must be lowercase SHA256')
    dsn = os.environ.get('PHOTO_TEST_DATABASE_URL')
    if not dsn:
        parser.error('PHOTO_TEST_DATABASE_URL required')
    with psycopg.connect(dsn) as db:
        db.execute('SET TRANSACTION READ ONLY')
        row = db.execute("""SELECT config_snapshot->'semantic_removal_preview'
            FROM photo.runs WHERE id=%s AND status='completed' AND graph_sealed""",
            (args.run,)).fetchone()
        if not row or not isinstance(row[0], dict):
            raise ValueError('completed removal preview required')
        config = row[0]
        params, actor, session = config['params'], config['actor_id'], config['session_id']
        if params['kind'] != 'remove_object':
            raise ValueError('wrong operation')
        # Resolve under the actual API role and saved actor/session. This rejects
        # historical/non-current previews, stale review state and revoked access.
        db.execute('SET LOCAL ROLE photo_api')
        current = db.execute('''SELECT preview_id FROM photo.lookup_semantic_removal_preview(
            %s,%s,%s,%s,%s,%s,%s,%s,%s)''',
            (params['tenant_id'], params['intent_id'], params['target_id'], actor, session,
             params['workflow_revision'], params['target_revision'], params['plan']['sha256'],
             params['candidate']['sha256'])).fetchone()
        if not current or current[0] != args.run:
            raise ValueError('requested run is not current authorized preview')
    payload = dict(params=params, actor=actor, session=session,
                   icc_lock_sha=args.icc_lock_sha, srgb_sha=args.srgb_sha)
    # Exclusive creation prevents overwriting another fixture or repository file.
    with output.open('x', encoding='utf-8') as stream:
        json.dump(payload, stream, indent=2)
        stream.write('\n')


if __name__ == '__main__':
    main()
