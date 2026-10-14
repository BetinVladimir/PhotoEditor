from dataclasses import replace
import pytest
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime.restore_quarantine import RestorePhysicalVersion,plan_restore_quarantine


def expected(key='b'):return RestoreObject('photos',key,'version','a'*64,5)
def physical(key='b'):return RestorePhysicalVersion('photos',key,'version',5)


def test_quarantine_plan_retains_referenced_versions_and_stages_only_extras():
    staged=[]
    result=plan_restore_quarantine([expected()],[physical('a'),physical(),physical('c')],staged.append,check_active=lambda:None)
    assert staged==[physical('a'),physical('c')]
    assert (result.referenced,result.physical,result.extra,result.extra_bytes)==(1,3,2,10)
    assert len(result.plan_sha256)==64


@pytest.mark.parametrize('mode',['missing','size','duplicate_expected','duplicate_physical','order','budget','count','stage_failure','cancelled'])
def test_quarantine_incomplete_comparisons_never_return_evidence(mode):
    wanted=[expected()];actual=[physical()];kwargs={};calls=0
    if mode=='missing':actual=[]
    if mode=='size':actual=[replace(physical(),size=6)]
    if mode=='duplicate_expected':wanted*=2
    if mode=='duplicate_physical':actual*=2
    if mode=='order':actual=[physical('z'),physical()]
    if mode=='budget':actual=[physical('a'),physical()];kwargs['maximum_extra_bytes']=4
    if mode=='count':actual=[physical(),physical('z')];kwargs['maximum_objects']=1
    if mode=='stage_failure':actual=[physical('a'),physical()]
    def stage(_):
        if mode=='stage_failure':raise RuntimeError('sink failed')
    def active():
        nonlocal calls
        calls+=1
        if mode=='cancelled' and calls==4:raise RuntimeError('cancelled')
    with pytest.raises((ValueError,RuntimeError)):
        plan_restore_quarantine(wanted,actual,stage,check_active=active,**kwargs)


def test_quarantine_explicit_false_refuses_before_inventory_consumption():
    consumed=[]
    def source():
        consumed.append(True)
        yield physical()
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        plan_restore_quarantine([],source(),lambda _:None,check_active=lambda:False)
    assert consumed==[]


def test_quarantine_stage_explicit_false_cannot_produce_sealed_plan():
    staged=[]
    def stage(version):staged.append(version);return False
    with pytest.raises(RuntimeError,match='RESTORE_QUARANTINE_STAGE_DENIED'):
        plan_restore_quarantine([], [physical('a')], stage,check_active=lambda:None)
    assert staged==[physical('a')]
