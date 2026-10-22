"""Real gateway tickets for the maximum deduplicated regional mask batch.

Authored only; execute with current migrations in the deep-test phase.
"""
from uuid import uuid4
from psycopg.types.json import Jsonb
from test_task_leases import queued_task, claim


def test_regional_batch_output_policy_and_per_role_limits(db):
    x = queued_task(db)
    command = {**x['command'], 'operation': 'mask.transform',
               'resource_class': 'cpu', 'timeout_seconds': 7200}
    db.execute("UPDATE photo.tasks SET operation='mask.transform',resource_class='cpu',"
               "dispatch_command=%s,execution_profile=execution_profile || %s WHERE id=%s",
               (Jsonb(command), Jsonb({'resource_estimate': {'output_bytes': 1024}}), x['task']))
    db.execute("UPDATE photo.worker_registrations SET resource_classes='[\"cpu\"]',"
               "qualified_operations='{\"mask.transform\":[\"cpu\"]}',certificate_sha256=%s WHERE worker_id=%s",
               ('f' * 64, x['worker']))
    db.execute("UPDATE photo.resource_slots SET resource_class='cpu' WHERE task_id=%s", (x['task'],))
    x['command'] = command
    assert claim(db, x)[0] == 'claimed'

    def begin(role, media):
        return db.execute('SELECT * FROM photo.begin_output(%s,1,1,%s,%s,%s,%s,2,%s,%s,%s)',
                          (x['task'], x['worker'], 'f' * 64, uuid4(), role,
                           'a' * 64, media, 'test')).fetchone()

    db.execute('SET LOCAL ROLE photo_gateway')
    for role, media in [('transformed_mask', 'application/octet-stream'),
                        ('transformed_mask_png', 'image/png')]:
        for _ in range(63):
            assert begin(role, media)[0] == 'created'
        assert begin(role, media)[0] == 'quota'
    assert begin('mask_transform_result', 'application/json')[0] == 'created'
    assert begin('mask_transform_result', 'application/json')[0] == 'quota'
    assert begin('transformed_mask', 'application/json')[0] == 'forbidden'
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.output_uploads WHERE task_id=%s',
                      (x['task'],)).fetchone()[0] == 127
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',
                      (x['tenant'],)).fetchone()[0] == 254
