"""Artifact pipeline boundaries, isolated from numerical/SQL implementation."""
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import UUID
import pytest
import numpy as np
from photo_worker.contracts.base import parse_json,canonical_bytes
from photo_worker.operations.semantic import preview_retouch as module
from photo_worker.operations.semantic.preview_retouch_adapter_binding import bind_retouch_preview_adapter
from test_v27_retouch_preview_context_loader import setup


@pytest.mark.parametrize('phase',['arrays','source','alpha','compute_enter','compute_exit'])
@pytest.mark.parametrize('loss',['cancel','lease','admission','qualification'])
def test_retouch_orchestration_refuses_lost_authority_at_each_phase(tmp_path,monkeypatch,phase,loss):
    run_pipeline(tmp_path,monkeypatch,phase,loss)


def test_retouch_orchestration_preserves_frozen_context_ref(tmp_path,monkeypatch):
    run_pipeline(tmp_path,monkeypatch,'arrays','mutate_ref')


def test_retouch_orchestration_owns_source_ref_passed_to_storage(tmp_path,monkeypatch):
    run_pipeline(tmp_path,monkeypatch,'source','mutate_storage_ref')


def test_retouch_orchestration_owns_numeric_input_ref_passed_to_storage(tmp_path,monkeypatch):
    run_pipeline(tmp_path,monkeypatch,'arrays','mutate_storage_input')


def run_pipeline(tmp_path,monkeypatch,phase,loss):
    ctx,params,context_ref,bodies=setup();digest=context_ref.sha256
    candidate_raw=bodies[UUID(params['candidate']['artifact_id'])]
    candidate=parse_json(candidate_raw)
    bound=bind_retouch_preview_adapter(params,candidate_raw,bodies[UUID(params['intent']['artifact_id'])],
        context_ref,bodies[context_ref.artifact_id],bodies[UUID(candidate['mask']['artifact_id'])])
    monkeypatch.setattr(module,'load_retouch_preview_face_context',lambda *args,**kwargs:(*bound,'image',('face',)))
    events=[];denied=False;unqualified=False
    def reached(current):
        nonlocal denied,unqualified
        events.append(current)
        if current!=phase:return
        if loss=='cancel':ctx.active=False
        if loss=='lease':ctx.lease.command.operation_version='2'
        if loss=='admission':denied=True
        if loss=='qualification':unqualified=True
        if loss=='mutate_ref':context_ref.sha256='0'*64
    class Artifacts:
        def declared_input_refs(self,maximum=512):return tuple(ctx.lease.command.inputs)
        scratch=object()
        def contract_bytes(self,ref,model):
            reached('source')
            if loss=='mutate_storage_ref':ref.sha256='0'*64
            return b'{}'
        @contextmanager
        def input(self,ref,**kwargs):
            assert loss=='mutate_storage_input'
            ref.sha256='0'*64
            yield tmp_path/'declared-input'
    def arrays(*args,**kwargs):
        reached('arrays')
        if loss=='mutate_storage_input':
            with args[0].input(bound[0].source.source) as local:
                assert local==tmp_path/'declared-input'
        return SimpleNamespace(original=np.zeros((1,2,3),np.float32),mask=np.zeros((1,2),np.float32),protected=np.zeros((1,2),np.float32))
    def alpha(*args,**kwargs):
        assert kwargs['max_bytes']==2**31-40
        reached('alpha');return None
    @contextmanager
    def compute(*args,**kwargs):
        assert kwargs['strength']==bound[2].strength
        try:
            reached('compute_enter');yield 'computed'
        finally:reached('compute_exit')
    monkeypatch.setattr(module,'load_retouch_preview_arrays',arrays)
    monkeypatch.setattr(module,'load_retouch_preview_alpha',alpha)
    monkeypatch.setattr(module,'compute_retouch_preview',compute)
    def qualification(lease,owned,ref,raw):
        assert ref.sha256==digest and raw==canonical_bytes(bound[3])
        return not unqualified
    frozen_params=canonical_bytes(bound[0])
    def admission(owner,value):
        assert canonical_bytes(value)==frozen_params
        return not denied
    def run():
        with module.execute_artifact_retouch_preview(ctx,Artifacts(),params,context_ref,
            verify_admission=admission,verify_qualification=qualification,
            verify_face_origin=lambda *args:True,verify_protection_origin=lambda *args:True,
            verify_color=lambda *args:True,max_ram_bytes=2**31) as value:
            assert value=='computed'
    if loss in ('mutate_storage_ref','mutate_storage_input'):run()
    else:
        with pytest.raises((ValueError,InterruptedError)):run()
    if phase=='arrays' and loss not in ('mutate_ref','mutate_storage_input'):assert events==['arrays']
    if phase=='source' and loss!='mutate_storage_ref':assert events==['arrays','source']
    if phase=='alpha':assert events==['arrays','source','alpha']
    if phase.startswith('compute'):assert events[-1]=='compute_exit'
