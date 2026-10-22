"""Installed heartbeat identity must win over matching unrelated function text.

Authored migration/schema scenario; not executed during the coding phase.
"""
from photo_worker.runtime.database import TaskDatabase


def test_unrelated_guard_copy_cannot_mask_an_unprotected_heartbeat(db,monkeypatch):
    database=TaskDatabase('unused')
    monkeypatch.setattr(database,'_query',lambda query,args:db.execute(query,args).fetchone())
    assert database.task_result_fence_ready() is True
    definition,source=db.execute("""SELECT pg_get_functiondef(oid),prosrc FROM pg_proc
      WHERE oid='photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)'::regprocedure""").fetchone()
    assert definition.count('FUNCTION photo.heartbeat_task(')==1
    copied=definition.replace('FUNCTION photo.heartbeat_task(',
                              'FUNCTION photo.heartbeat_guard_unrelated_copy(',1)
    db.execute(copied)
    # Retain the public function identity, return type and ACL; remove its actual
    # implementation while a different function still contains every guard.
    assert definition.count(source)==1
    db.execute(definition.replace(source,
        "BEGIN RETURN QUERY SELECT false,NULL::timestamptz,false;END",1))
    assert database.task_result_fence_ready() is False
