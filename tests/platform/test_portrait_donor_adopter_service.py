from types import SimpleNamespace
from contextlib import nullcontext
from threading import Event
import pytest
from photo_worker.v2 import service
from photo_worker.v2.portrait_donor_adoption_prepare import DonorAdoptionInstallation
from photo_worker.operations.portrait.donor_adapter import DonorRuntimeOptions
from test_portrait_donor_runtime_lock import current_runtime


def role_connection():
    def execute(query, params=()):
        assert "current_user=" in query
        return SimpleNamespace(fetchone=lambda:(True,))
    return SimpleNamespace(autocommit=True,execute=execute)


def test_service_carries_owned_donor_installation_to_adopter(tmp_path,monkeypatch):
    config=service.AdopterConfig('postgres://unused','https://objects.example','bucket','key','secret')
    policy={'owned':True}
    installation=DonorAdoptionInstallation(policy,DonorRuntimeOptions(current_runtime().model_copy(deep=True),
        'b'*64,tmp_path,tmp_path,100,200),tmp_path,300,1<<30)
    connections=[role_connection(),role_connection()];seen=[]
    def connect(*args,**kwargs):
        assert kwargs['autocommit'] is True and 'role=photo_scheduler' in kwargs['options']
        return nullcontext(connections.pop(0))
    def objects(*args,**kwargs):
        assert kwargs['http_client'].connection_pool_kw['retries'].total is False
        policy.clear();return object()
    monkeypatch.setattr(service.psycopg,'connect',connect)
    monkeypatch.setattr(service,'Minio',objects)
    monkeypatch.setattr(service,'Adopter',lambda *args,**kwargs:seen.append(kwargs))
    stop=Event();stop.set()
    service.run_adopter(config,stop,donor_installation=installation)
    assert seen[0]['donor_installation'].policy=={'owned':True} and not connections


def test_invalid_donor_installation_refused_before_external_io(monkeypatch):
    config=service.AdopterConfig('postgres://unused','https://objects.example','bucket','key','secret')
    def forbidden(*args,**kwargs):raise AssertionError('external IO')
    monkeypatch.setattr(service,'Minio',forbidden)
    monkeypatch.setattr(service.psycopg,'connect',forbidden)
    with pytest.raises(ValueError,match='donor adopter'):
        service.run_adopter(config,donor_installation=object())
