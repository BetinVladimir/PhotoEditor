"""The adopter processes only V2 candidates and keeps invalid results fenced."""
from contextlib import nullcontext
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.service import Adopter, AdopterConfig


class Cursor:
    def __init__(self, rows):
        self.rows = rows

    def fetchall(self):
        return self.rows

    def fetchone(self):
        return self.rows[0]


class ReadConnection:
    autocommit = True

    def __init__(self, tenant, run, task):
        self.tenant, self.run, self.task = tenant, run, task
        self.unlocked = False

    def execute(self, query, params=()):
        if "v2_pending_adoptions" in query:
            return Cursor([(self.tenant, self.run, self.task)])
        if "pg_try_advisory_lock" in query:
            return Cursor([(True,)])
        if "pg_advisory_unlock" in query:
            self.unlocked = True
            return Cursor([(True,)])
        if "v2_pending_finalizations" in query:
            return Cursor([(self.tenant, self.run)])
        raise AssertionError(query)


class WriteConnection:
    def __init__(self, state="completed"):
        self.state = state
        self.rejected = False

    def transaction(self):
        return nullcontext()

    def execute(self, query, params):
        if "reject_v2_adoption" in query:
            self.rejected = True
            return Cursor([(True,)])
        if "finalize_v2_run" in query:
            return Cursor([(self.state,)])
        raise AssertionError(query)


def test_adopter_processes_candidate_and_finalizes():
    tenant, run, task = uuid4(), uuid4(), uuid4()
    read, write = ReadConnection(tenant, run, task), WriteConnection()
    adopter = Adopter(read, write, object(), "bucket")
    adopter.coordinator = SimpleNamespace(adopt=lambda *_: True)
    assert adopter.run_once() == {"adopted": 1, "rejected": 0, "finalized": 1, "retry": 0}
    assert read.unlocked


def test_invalid_result_is_rejected_then_failed():
    tenant, run, task = uuid4(), uuid4(), uuid4()
    read, write = ReadConnection(tenant, run, task), WriteConnection("failed")
    adopter = Adopter(read, write, object(), "bucket")
    result = uuid4()

    def invalid(*_):
        raise AdoptionInvalid("ARTIFACT_SCHEMA_INVALID")

    adopter.coordinator = SimpleNamespace(
        adopt=invalid, context=lambda *_: SimpleNamespace(result_id=result))
    assert adopter.run_once() == {"adopted": 0, "rejected": 1, "finalized": 1, "retry": 0}
    assert write.rejected and read.unlocked


def test_adopter_config_requires_production_https():
    values = {"DATABASE_URL": "postgres://unused", "S3_ENDPOINT": "http://minio:9000",
              "S3_BUCKET": "bucket", "S3_ACCESS_KEY": "key", "S3_SECRET_KEY": "secret"}
    with pytest.raises(ValueError, match="endpoint"):
        AdopterConfig.from_env(values)
    values["APP_ENV"] = "development"
    values["S3_ENDPOINT"] = "http://127.0.0.1:9000"
    assert AdopterConfig.from_env(values).development


@pytest.mark.parametrize('code',['LAYER_ADOPTION_SCRATCH_REQUIRED','ADOPTION_SCRATCH_EXCEEDED'])
def test_scratch_configuration_failure_retries_without_rejecting_result(code):
    tenant,run,task=uuid4(),uuid4(),uuid4()
    read=ReadConnection(tenant,run,task);write=WriteConnection()
    adopter=Adopter(read,write,object(),'test')
    def fail(*args):raise AdoptionInvalid(code)
    adopter.coordinator=SimpleNamespace(adopt=fail)
    assert adopter.run_once()=={'adopted':0,'rejected':0,'finalized':1,'retry':1}


def test_layer_scratch_configuration_is_paired_and_bounded(tmp_path):
    values={'DATABASE_URL':'postgresql://example','S3_ENDPOINT':'http://localhost:9000',
        'S3_BUCKET':'test','S3_ACCESS_KEY':'test','S3_SECRET_KEY':'test','APP_ENV':'development'}
    for budget in ('','0','-1','9007199254740992'):
        with pytest.raises(ValueError):AdopterConfig.from_env({**values,'PHOTO_ADOPTER_LAYER_SCRATCH_ROOT':str(tmp_path),'PHOTO_ADOPTER_LAYER_SCRATCH_BYTES':budget})
    config=AdopterConfig.from_env({**values,'PHOTO_ADOPTER_LAYER_SCRATCH_ROOT':str(tmp_path),'PHOTO_ADOPTER_LAYER_SCRATCH_BYTES':'1000000'})
    assert config.layer_scratch_root==tmp_path and config.layer_scratch_bytes==1000000
