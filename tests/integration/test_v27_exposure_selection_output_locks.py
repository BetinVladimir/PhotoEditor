"""Current exposure selection locks across concurrent DB sessions; unexecuted."""
import json
import psycopg
import pytest
from semantic_mixed_exposure_fixture import mixed_decision_fixture


def test_exposure_selection_retains_output_and_result_locks_until_transaction_end(database):
    with psycopg.connect(**database) as seed:
        f = mixed_decision_fixture(seed)
        args,run = f['selected'][0]['data'][:2]
        params = json.loads(args[5])
        receipt = seed.execute('SELECT v27_preapply_preview_receipt FROM photo.runs WHERE id=%s', (run,)).fetchone()[0]
        values = (args[0],args[1],args[2],args[3],int(params['workflow_revision']),args[4],int(params['target_revision']),
                  params['plan']['sha256'],params['candidate']['sha256'],receipt['working_image']['artifact_id'],receipt['working_image']['sha256'],receipt['presentation']['artifact_id'],receipt['presentation']['sha256'])
    query = 'SELECT * FROM photo.lock_semantic_preapply_preview_selection('+','.join(['%s']*13)+')'
    with psycopg.connect(**database) as reader, psycopg.connect(**database) as writer:
        reader.execute('SET LOCAL ROLE photo_semantic_admission')
        assert len(reader.execute(query, values).fetchall()) == 1
        for key in ('presentation','working_image','pixels','numerical_evidence'):
            with pytest.raises(psycopg.errors.LockNotAvailable):
                with writer.transaction():
                    writer.execute("SET LOCAL lock_timeout='100ms'")
                    writer.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                                   (args[0],receipt[key]['artifact_id']))
        with pytest.raises(psycopg.errors.LockNotAvailable):
            with writer.transaction():
                writer.execute("SET LOCAL lock_timeout='100ms'")
                writer.execute("UPDATE photo.task_results SET metrics=metrics||'{\"writer\":true}'::jsonb WHERE tenant_id=%s AND id=%s",
                               (args[0],receipt['result_id']))
        reader.rollback()
        writer.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                       (args[0],receipt['working_image']['artifact_id']))
        writer.commit()
        with pytest.raises(psycopg.errors.SerializationFailure):
            with reader.transaction():
                reader.execute('SET LOCAL ROLE photo_semantic_admission')
                reader.execute(query, values)
