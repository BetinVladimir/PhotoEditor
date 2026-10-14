from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.restore_render_preview_catalogue import (
    observe_fixture_after_preview_objects,observe_fixture_before_asset_preview,
    observe_fixture_before_render_preview)


@pytest.mark.parametrize('mode',['valid','missing','duplicate','foreign_bucket','bad_media',
                                 'foreign_database','cancelled','fixture_false','activity_false'])
def test_after_preview_catalogue_requires_exact_adopted_receipt_and_versions(mode):
    tenant,review,task=(uuid4() for _ in range(3))
    source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    manifest,image=uuid4(),uuid4()
    row=(manifest,'b'*64,'other' if mode=='foreign_bucket' else 'photos',
         'manifest.json','m1',100,'application/json',image,'c'*64,
         'photos','image.jpg','i1',1000,
         'text/plain' if mode=='bad_media' else 'image/jpeg')
    rows=[] if mode=='missing' else [row,row] if mode=='duplicate' else [row]
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,params=None):
            queries.append(sql)
            if 'FROM photo.reviews v' in sql:
                assert "v.state='pending'" in sql and "r.status='completed'" in sql
                assert "v.state='decided'" in sql and "v.decision='accept'" in sql
                assert 'mu.object_version=m.object_version' in sql
                assert 'origin.id=v.task_id AND origin.run_id=workflow.run_id' in sql
                assert 'r.v26_derivative_task_id=origin.id' in sql
                assert params==(tenant,review,task,None,None,None,
                                source.artifact_id,source.sha256,
                                source.artifact_id,source.sha256,'preview1600','d'*64)
                return NS(fetchall=lambda:rows)
            if sql.startswith('SELECT current_database'):
                return NS(fetchone=lambda:('foreign' if mode=='foreign_database' else 'isolated',True))
            return NS()
    def active():
        if mode=='cancelled':raise RuntimeError('cancelled')
        if mode=='activity_false':return False
    def run():return observe_fixture_after_preview_objects(Connection(),tenant,review,task,
        source,'preview1600','d'*64,('photos',),expected_database='isolated',
        assert_fixture_scope=lambda:False if mode=='fixture_false' else None,check_active=active)
    if mode=='valid':
        observed=run()
        assert observed.manifest.artifact_id==manifest and observed.image.artifact_id==image
        assert observed.manifest_object.version=='m1' and observed.image_object.version=='i1'
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('fixture_false','activity_false'):assert queries==[]


def test_after_preview_catalogue_can_reobserve_only_accepted_revision():
    tenant,review,task=(uuid4() for _ in range(3))
    source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    row=(uuid4(),'b'*64,'photos','manifest.json','m1',100,'application/json',
         uuid4(),'c'*64,'photos','image.jpg','i1',1000,'image/jpeg')
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,params=None):
            if 'FROM photo.reviews v' in sql:
                assert params==(tenant,review,task,2,2,2,
                                source.artifact_id,source.sha256,
                                source.artifact_id,source.sha256,'preview1600','d'*64)
                assert "v.decision_route='candidate'" in sql
                return NS(fetchall=lambda:[row])
            if sql.startswith('SELECT current_database'):
                return NS(fetchone=lambda:('isolated',True))
            return NS()
    observed=observe_fixture_after_preview_objects(Connection(),tenant,review,task,source,
        'preview1600','d'*64,('photos',),expected_database='isolated',accepted_revision=2,
        assert_fixture_scope=lambda:None,check_active=lambda:None)
    assert observed.image_object.version=='i1'


@pytest.mark.parametrize('mode',['valid','missing','duplicate','foreign_bucket','foreign_database','fixture_false','activity_false'])
def test_before_preview_catalogue_requires_live_exact_original_asset(mode):
    tenant,review=(uuid4() for _ in range(2))
    source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    manifest,image=uuid4(),uuid4()
    row=(manifest,'b'*64,'other' if mode=='foreign_bucket' else 'photos',
         'before-manifest.json','m1',100,'application/json',image,'c'*64,
         'photos','before.jpg','i1',1000,'image/jpeg')
    rows=[] if mode=='missing' else [row,row] if mode=='duplicate' else [row]
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,params=None):
            queries.append(sql)
            if 'FROM photo.reviews review' in sql:
                assert "review.state='pending'" in sql
                assert "v.review_state='accepted'" in sql
                assert 'mu.object_version=m.object_version' in sql
                assert params==(tenant,review,source.artifact_id,source.artifact_id,
                                source.sha256,'preview1600','d'*64,
                                source.artifact_id,source.sha256)
                return NS(fetchall=lambda:rows)
            if sql.startswith('SELECT current_database'):
                return NS(fetchone=lambda:('foreign' if mode=='foreign_database' else 'isolated',True))
            return NS()
    def run():return observe_fixture_before_asset_preview(Connection(),tenant,review,source,
        'preview1600','d'*64,('photos',),expected_database='isolated',
        assert_fixture_scope=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':
        observed=run();assert observed.source==source
        assert observed.manifest_object.version=='m1' and observed.image_object.version=='i1'
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('fixture_false','activity_false'):assert queries==[]


@pytest.mark.parametrize('mode',['valid','missing','foreign_bucket','foreign_database','fixture_false','activity_false'])
def test_before_render_preview_uses_review_base_and_same_project(mode):
    tenant,review=uuid4(),uuid4()
    source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    row=(uuid4(),'b'*64,'other' if mode=='foreign_bucket' else 'photos',
         'manifest.json','m1',100,'application/json',uuid4(),'c'*64,
         'photos','image.jpg','i1',1000,'image/jpeg')
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,params=None):
            queries.append(sql)
            if 'FROM photo.reviews v' in sql:
                assert "v.base_artifact_id=%s" in sql
                assert 'preview_workflow.project_id=w.project_id' in sql
                assert "v.state='pending'" in sql
                assert 'mu.object_version=m.object_version' in sql
                assert params==(tenant,review,source.artifact_id,source.artifact_id,
                                source.sha256,'preview1600','d'*64)
                return NS(fetchall=lambda:[] if mode=='missing' else [row])
            if sql.startswith('SELECT current_database'):
                return NS(fetchone=lambda:('foreign' if mode=='foreign_database' else 'isolated',True))
            return NS()
    def run():return observe_fixture_before_render_preview(Connection(),tenant,review,source,
        'preview1600','d'*64,('photos',),expected_database='isolated',
        assert_fixture_scope=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':
        result=run();assert result.source==source and result.image_object.version=='i1'
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('fixture_false','activity_false'):assert queries==[]
