"""A qualified series lease stops and cannot publish after device rotation."""

from psycopg.types.json import Jsonb
from test_task_leases import queued_task, manifest


def test_series_lease_rotation_stops_heartbeat_and_success(db):
    fixture = queued_task(db)
    model, runtime, device = 'a' * 64, 'b' * 64, 'c' * 64
    profile = dict(backend='cpu', precision='float32', model_lock_id=runtime,
                   model_lock_sha=model, runtime_lock_sha=runtime,
                   device_profile_sha=device)
    command = {**fixture['command'], 'operation': 'series.balance',
               'resource_class': 'cpu'}
    db.execute("UPDATE photo.tasks SET operation='series.balance',"
               "resource_class='cpu',execution_profile=%s,dispatch_command=%s "
               "WHERE id=%s", (Jsonb(profile), Jsonb(command), fixture['task']))
    db.execute("UPDATE photo.resource_slots SET resource_class='cpu' "
               "WHERE task_id=%s", (fixture['task'],))
    db.execute("UPDATE photo.worker_registrations SET "
               "resource_classes='[\"cpu\"]',"
               "qualified_operations='{\"series.balance\":[\"cpu\"]}',"
               "lock_id=%s,v23_series_device_profile_sha=%s "
               "WHERE worker_id=%s", (runtime, device, fixture['worker']))
    db.execute('SET LOCAL ROLE photo_worker')
    claimed = db.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',
                         (fixture['task'], fixture['worker'], Jsonb(command))).fetchone()
    assert claimed[0] == 'claimed'
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.worker_registrations SET '
               'v23_series_device_profile_sha=%s WHERE worker_id=%s',
               ('d' * 64, fixture['worker']))
    db.execute('SET LOCAL ROLE photo_worker')
    beat = db.execute('SELECT * FROM photo.heartbeat_task(%s,1,%s,%s,%s)',
                      (fixture['task'], claimed[1], fixture['worker'],
                       Jsonb(dict(done=1,total=2)))).fetchone()
    assert beat == (False, None, False)
    committed = db.execute('SELECT * FROM photo.commit_task_success(%s,1,%s,%s,%s)',
                           (fixture['task'], claimed[1], fixture['worker'],
                            manifest())).fetchone()
    assert committed == ('stale', None, None)
