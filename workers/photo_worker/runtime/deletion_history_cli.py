"""Bounded stdin bridge for operator-installed independent deletion storage."""
import argparse
from datetime import datetime
import sys
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes,parse_json
from .deletion_history_store import DeletionHistoryStore


def append_request(raw,store):
    request=parse_json(raw,max_bytes=4096)
    if (not isinstance(request,dict) or set(request)!={'schema_version','event_id','tenant_id','occurred_at'}
            or type(request['schema_version']) is not int or request['schema_version']!=1
            or canonical_bytes(request)!=raw):
        raise ValueError('DELETION_HISTORY_REQUEST')
    event,tenant=UUID(request['event_id']),UUID(request['tenant_id'])
    if str(event)!=request['event_id'] or str(tenant)!=request['tenant_id']:
        raise ValueError('DELETION_HISTORY_REQUEST')
    occurred=datetime.fromisoformat(request['occurred_at'].replace('Z','+00:00'))
    result=store.append(event,tenant,'requested',occurred)
    return canonical_bytes({'schema_version':1,'event_id':str(event),'tenant_id':str(tenant),
        'sequence':result.sequence,'head_sha256':result.head_sha256})


def main():
    parser=argparse.ArgumentParser()
    root=parser.add_mutually_exclusive_group(required=True)
    root.add_argument('--root')
    root.add_argument('--root-fd',type=int)
    args=parser.parse_args()
    try:
        raw=sys.stdin.buffer.read(4097)
        if not 2<=len(raw)<=4096:raise ValueError('DELETION_HISTORY_REQUEST')
        store=DeletionHistoryStore(args.root,check_active=lambda:None,root_fd=args.root_fd)
        result=append_request(raw,store)
        sys.stdout.buffer.write(result);sys.stdout.buffer.flush()
    except Exception:
        # Never echo journal paths, database errors or request fields.
        return 1
    return 0


if __name__=='__main__':raise SystemExit(main())
