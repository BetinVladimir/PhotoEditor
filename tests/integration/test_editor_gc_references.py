"""Durable editor columns retain referenced objects independently of task refs."""
from uuid import uuid4
import pytest
from psycopg import sql


@pytest.mark.parametrize('column,sha_column', [
    ('source_artifact_id','source_sha'),
    ('frozen_recipe_artifact_id','frozen_recipe_sha'),
    ('executed_style_artifact_id','executed_style_sha'),
    ('processed_source_artifact_id','processed_source_sha'),
    ('quality_artifact_id','quality_sha'),
    ('result_artifact_id','result_sha'),
    ('seal_params_artifact_id','seal_params_sha'),
    ('seal_receipt_artifact_id','seal_receipt_sha'),
    ('effect_source_artifact_id','effect_source_sha'),
    ('effect_source_result_artifact_id','effect_source_result_sha'),
    ('final_recipe_artifact_id',None),
])
def test_editor_build_reference_is_a_gc_root_and_rejects_nonready_replacement(db,column,sha_column):
    from editor_admitted_final_fixture import admitted_final
    tenant,origin,*_ = admitted_final(db)
    artifact = uuid4()
    digest = artifact.hex*2
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES(%s,%s,'fixture',%s,'v1',%s,2,'application/json','ready',clock_timestamp())", (artifact,tenant,str(artifact),digest))
    assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)', (tenant,artifact)).fetchone() == (False,)
    assignments = sql.SQL('{}=%s').format(sql.Identifier(column))
    args = [artifact]
    if sha_column:
        assignments += sql.SQL(',{}=%s').format(sql.Identifier(sha_column))
        args.append(digest)
    query = sql.SQL('UPDATE photo.editor_effect_builds SET {} WHERE tenant_id=%s AND run_id=%s').format(assignments)
    args += [tenant,origin]
    db.execute(query,args)
    db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s", (artifact,))
    assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)', (tenant,artifact)).fetchone() == (True,)
    assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)', (uuid4(),artifact)).fetchone() == (False,)
    # A distinct non-ready object cannot become a new durable reference.
    invalid = uuid4()
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES(%s,%s,'fixture',%s,'v1',%s,2,'application/json','ready',clock_timestamp())", (invalid,tenant,str(invalid),'f'*64))
    db.execute("UPDATE photo.artifacts SET state='deleting' WHERE id=%s", (invalid,))
    invalid_args = [invalid] + (['f'*64] if sha_column else []) + [tenant,origin]
    with pytest.raises(Exception,match='ARTIFACT_REFERENCE_NOT_READY'):
        with db.transaction():
            db.execute(query,invalid_args)
