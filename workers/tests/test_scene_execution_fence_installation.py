"""Authored source installation contract, not model or photograph qualification."""
import pytest
from photo_worker.runtime.database import TaskDatabase

@pytest.mark.parametrize('row,want', [(None,False), ((False,),False), ((1,),False), ((True,),True)])
def test_result_fence_requires_exact_scene_execution_conjunction(row,want):
    db=TaskDatabase('unused')
    calls=[]
    def query(sql,args):
        calls.append((sql,args))
        return row
    db._query=query
    assert db.task_result_fence_ready() is want
    assert len(calls)==1 and calls[0][1]==()
    sql=calls[0][0]
    assert 'AND photo.semantic_scene_task_live(p_task) IS TRUE' in sql
    assert "count(*)=5" in sql
    assert "op.v2_enabled" in sql
    assert "photo.semantic_scene_source_pins_live" in sql
    assert "photo.semantic_scene_qualification_live" in sql
    assert "photo.semantic_scene_params_live" in sql
    assert "FOR UPDATE" in sql and "FOR SHARE" in sql
    assert "has_function_privilege(role.name,scene.oid,'EXECUTE')" in sql
