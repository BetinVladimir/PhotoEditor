from hashlib import sha256
from types import SimpleNamespace
import pytest
from photo_worker.operations.interchange.recipe_factory import PreparedImportRecipeFactory
from photo_worker.v2.adoption import AdoptionInvalid

@pytest.mark.parametrize('change',['schema','icc','runtime','budget','validator'])
def test_recipe_factory_requires_pinned_installation(change):
    schema=b'{"$defs":{}}'
    args=dict(schemas_jcs=schema,schemas_sha=sha256(schema).hexdigest(),work_icc_sha='a'*64,qualify_runtime=lambda *args:True)
    if change=='schema':args['schemas_sha']='b'*64
    if change=='icc':args['work_icc_sha']='invalid'
    if change=='runtime':args['qualify_runtime']=None
    if change=='budget':args['max_file_bytes']=True
    if change=='validator':args['extra_validators']={'Params':None}
    with pytest.raises(ValueError,match='INSTALLATION'):PreparedImportRecipeFactory(**args)

def test_recipe_factory_checks_task_schema_before_input_io():
    schema=b'{"$defs":{}}'
    factory=PreparedImportRecipeFactory(schemas_jcs=schema,schemas_sha=sha256(schema).hexdigest(),work_icc_sha='a'*64,qualify_runtime=lambda *args:True)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={'contracts_sha':'0'*64}))
    with pytest.raises(AdoptionInvalid,match='SCHEMA_PIN'):factory(ctx,None,None)


@pytest.mark.parametrize('change',['none','params','plan_contract','master_contract'])
def test_recipe_factory_composes_owned_sources_and_checks_pins(monkeypatch,change):
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.operations.session.contracts import VersionBinding
    from photo_worker.v2.input_closure import InputClosureEntry
    from photo_worker.v2.interchange_plan import InterchangeImportParams
    from test_interchange_import_stage import adopted_fixture
    import photo_worker.operations.interchange.recipe_factory as module
    _,sources,_,image_raw=adopted_fixture()
    source=sources.entries[0]
    image=WorkingImage.model_validate_json(image_raw)
    binding=VersionBinding(asset_version_id=source.asset_version_id,source=source.master,canvas_hash=canonical_hash(image.canvas))
    plan_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(b'{}').hexdigest())
    params=InterchangeImportParams(schema_version=1,parsed=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),plan=plan_ref,
        evidence=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),max_report_bytes=8388608)
    class Reader:
        def __init__(self,*args,**kwargs):
            self.budget=kwargs["read_budget"]
            self.read_budget=self.budget
            self.entries={(plan_ref.artifact_id,plan_ref.sha256):InputClosureEntry(ref=plan_ref,contract='Wrong' if change=='plan_contract' else 'InterchangePlan'),
                (binding.source.artifact_id,binding.source.sha256):InputClosureEntry(ref=binding.source,contract='Wrong' if change=='master_contract' else 'WorkingImage')}
        def read_json(self,ref,maximum):
            raw = image_raw if ref==binding.source else b'{}'
            assert len(raw) <= maximum
            self.budget.consume(len(raw), is_json=True)
            return raw
        def evidence(self,ref):return SimpleNamespace(media_type='application/json')
        def recheck(self):pass
    monkeypatch.setattr(module,'PreparedImportInputReader',Reader)
    from test_interchange_export_prepare import fixture as export_fixture
    plan,_,_=export_fixture()
    plan.direction='import';plan.project_id=sources.project_id;plan.snapshot_digest='f'*64
    plan.entries[0].binding=binding
    monkeypatch.setattr(module,'read_interchange_plan',lambda *args,**kwargs:plan)
    captured={}
    sentinel=lambda *args:True
    def build(**kwargs):captured.update(kwargs);return sentinel
    monkeypatch.setattr(module,'build_prepared_import_recipe_qualifier',build)
    schemas=b'{"$defs":{}}';digest=sha256(schemas).hexdigest()
    factory=PreparedImportRecipeFactory(schemas_jcs=schemas,schemas_sha=digest,work_icc_sha=image.color.icc_sha256,qualify_runtime=lambda *args:True)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={'contracts_sha':digest},command=SimpleNamespace(params=SimpleNamespace(sha256='0'*64 if change=='params' else sha256(canonical_bytes(params)).hexdigest()))))
    artifacts=SimpleNamespace(params=lambda model:params,scratch=object())
    authority=SimpleNamespace(max_input_json_bytes=1024*1024,project_id=sources.project_id,snapshot_digest='f'*64)
    if change!='none':
        with pytest.raises(AdoptionInvalid):factory(ctx,artifacts,authority)
        assert not captured
    else:
        assert factory(ctx,artifacts,authority) is sentinel
        assert captured['canonical_images'][0][0]==binding
        assert captured['canonical_images'][0][1]==image
        assert len(captured['entries'])==2
        assert captured['qualify_runtime']('WorkingImage',image,binding) is True

@pytest.mark.parametrize('change', ['none', 'project', 'snapshot', 'schema', 'budget', 'occurrence'])
def test_export_recipe_factory_uses_shared_reader_budget_and_plan_authority(monkeypatch, change):
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.v2.interchange_read_budget import ImportReadBudget
    from test_interchange_import_stage import adopted_fixture
    from test_interchange_export_prepare import fixture
    import photo_worker.operations.interchange.recipe_factory as module
    _, sources, _, raw = adopted_fixture();image = WorkingImage.model_validate_json(raw)
    plan, _, _ = fixture();plan.project_id = sources.project_id
    plan.entries[0].binding.source = sources.entries[0].master
    plan.entries[0].binding.asset_version_id = sources.entries[0].asset_version_id
    from photo_worker.contracts.base import canonical_hash
    plan.entries[0].binding.canvas_hash = canonical_hash(image.canvas)
    key = (sources.entries[0].master.artifact_id, sources.entries[0].master.sha256)
    class Reader:
        entries = {key:SimpleNamespace(ref=sources.entries[0].master,contract='WorkingImage')}
        read_budget = ImportReadBudget(1024)
        calls = 0
        def read_json(self, reference, maximum):
            self.calls += 1;assert maximum <= self.read_budget.remaining_json_bytes
            self.read_budget.consume(len(raw),is_json=True);return raw
        def evidence(self, reference):return SimpleNamespace(media_type='application/json')
        def recheck(self):pass
    reader=Reader();monkeypatch.setattr(module,'PreparedImportInputReader',Reader)
    schema=b'{"$defs":{}}';digest=sha256(schema).hexdigest()
    factory=PreparedImportRecipeFactory(schemas_jcs=schema,schemas_sha=digest,work_icc_sha='e'*64,qualify_runtime=lambda *args:True)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={'contracts_sha':digest}))
    authority=SimpleNamespace(max_input_json_bytes=1024,project_id=plan.project_id,snapshot_digest=plan.snapshot_digest)
    if change=='project':authority.project_id=__import__('uuid').uuid4()
    elif change=='snapshot':authority.snapshot_digest='f'*64
    elif change=='schema':ctx.lease.execution_profile['contracts_sha']='f'*64
    elif change=='budget':authority.max_input_json_bytes=2048
    captured={};sentinel=object()
    def build(**kwargs):captured.update(kwargs);return sentinel
    monkeypatch.setattr(module,'build_prepared_import_recipe_qualifier',build)
    if change not in ('none','occurrence'):
        with pytest.raises(AdoptionInvalid):factory.build_for_plan(ctx,SimpleNamespace(scratch=object()),authority,plan,reader)
        assert reader.calls==0 and not captured
    else:
        occurrence=lambda event: True
        kwargs={} if change=='none' else dict(qualify_occurrence=occurrence)
        assert factory.build_for_plan(ctx,SimpleNamespace(scratch=object()),authority,plan,reader,**kwargs) is sentinel
        assert captured['qualify_occurrence'] is (None if change=='none' else occurrence)
        assert reader.calls==1 and captured['evidence_reader'] is reader
        assert captured['canonical_images'][0][1]==image
        assert reader.read_budget.remaining_json_bytes==1024-len(raw)


@pytest.mark.parametrize('registry', [{('custom','1'):'Missing'}, {('a'*81,'1'):'Params'}, []])
def test_factory_rejects_bad_operator_installation_before_task_io(registry):
    schema=b'{"$defs":{"Params":{}}}'
    with pytest.raises(ValueError, match='OPERATORS'):
        PreparedImportRecipeFactory(schemas_jcs=schema,schemas_sha=sha256(schema).hexdigest(),
            work_icc_sha='a'*64,qualify_runtime=lambda *args:True,operation_schemas=registry)
