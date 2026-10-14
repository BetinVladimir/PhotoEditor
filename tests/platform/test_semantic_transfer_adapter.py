from contextlib import contextmanager
from types import SimpleNamespace
import pytest
from photo_worker.operations.semantic import adapter
from photo_worker.runtime.executor import OperationFailure,OperationResult


@pytest.mark.parametrize('variant',['valid','backend','renderer','installation'])
def test_transfer_dispatcher_pins_operation_roles_and_dependencies(monkeypatch,variant):
    events=[];config=object();artifacts=object();result=OperationResult([])
    profile={'backend':'cuda','renderer_version':'v2.2'}
    if variant=='backend':profile['backend']='cpu'
    if variant=='renderer':profile['renderer_version']='v1'
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile=profile))
    installation=adapter.SemanticTransferInstallation(lambda:None,lambda:None,lambda:None)
    def factory(value):
        assert value is ctx;events.append('install')
        return None if variant=='installation' else installation
    @contextmanager
    def create(value,settings,roles):
        assert value is ctx and settings is config
        assert roles==frozenset({'document','mask','mask_pixels','transform'})
        events.append('open')
        try:yield artifacts
        finally:events.append('close')
    def execute(value,inputs,**kwargs):
        assert value is ctx and inputs is artifacts
        assert kwargs==dict(load_arrays=installation.load_arrays,verify_runtime=installation.verify_runtime,
            verify_scene=installation.verify_scene)
        events.append('execute');return result
    monkeypatch.setattr(adapter.TaskArtifacts,'create',create)
    monkeypatch.setattr(adapter,'execute_transfer',execute)
    operation,=adapter.semantic_transfer_operations(config,installation_factory=factory)
    assert (operation.name,operation.resource_class,operation.version)==('semantic.transfer','gpu','1')
    if variant=='valid':
        assert operation.execute(ctx) is result
        assert events==['install','open','execute','close']
    else:
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):operation.execute(ctx)
        assert events==(['install'] if variant=='installation' else [])


def test_transfer_installation_requires_every_authority():
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        adapter.semantic_transfer_operations(object(),installation_factory=None)
    with pytest.raises(ValueError,match='INSTALLATION_INVALID'):
        adapter.SemanticTransferInstallation(lambda:None,None,lambda:None)


def test_qualified_installer_uses_live_exact_report_and_keeps_source_owners():
    from test_semantic_qualification_loader import loader_fixture
    from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
    from photo_worker.v2.adoption import AdoptionInvalid
    db,objects,record,_=loader_fixture()
    pin=SemanticQualificationPin.from_value(record['pin'])
    ctx=SimpleNamespace(check_active=lambda:None,lease=object())
    arrays=lambda:None;scene=lambda:None
    installation=adapter.qualified_semantic_transfer_installation(ctx,database=db,objects=objects,bucket='owned',
        frozen_pin=pin,model_lock_sha=pin.model_lock_sha,runtime_lock_sha=pin.runtime_lock_sha,
        device_profile_sha=pin.device_profile_sha,dataset_sha=pin.dataset_sha,load_arrays=arrays,verify_scene=scene)
    assert installation.load_arrays is arrays and installation.verify_scene is scene
    assert installation.verify_runtime()==pin
    record['policy']['required_checks']=['changed']
    with pytest.raises(AdoptionInvalid):installation.verify_runtime()


@pytest.mark.parametrize('variant',['valid','qualification_revoked','protection_revoked','origin_denied'])
def test_sql_qualified_installer_composes_report_sources_and_protection(tmp_path,variant):
    from test_semantic_qualification_loader import loader_fixture
    from test_semantic_source_authority import fixture
    from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
    from photo_worker.v2.adoption import AdoptionInvalid
    db,objects,record,_=loader_fixture()
    source,colors,params,image,lease,raw,_=fixture(tmp_path)
    pin=SemanticQualificationPin.from_value(record['pin'])
    db.semantic_source_ready=source.semantic_source_ready
    protections={'source_protection':params.reference.source.model_dump(mode='json'),
                 'target_protection':params.target.source.model_dump(mode='json')}
    calls=[]
    def pins(owner):calls.append('pins');return protections
    db.semantic_protection_pins=pins
    ctx=SimpleNamespace(check_active=lambda:None,lease=lease)
    scene=lambda:None
    def origin(*values):
        calls.append('origin')
        if variant=='origin_denied':raise AdoptionInvalid('ORIGIN_DENIED')
    if variant=='qualification_revoked':record['policy']['admission_enabled']=False
    kwargs=dict(database=db,objects=objects,bucket='owned',frozen_pin=pin,
        model_lock_sha=pin.model_lock_sha,runtime_lock_sha=pin.runtime_lock_sha,
        device_profile_sha=pin.device_profile_sha,dataset_sha=pin.dataset_sha,
        params_bytes=raw,verify_color=colors.append,verify_protection=origin,verify_scene=scene)
    if variant=='qualification_revoked':
        with pytest.raises(AdoptionInvalid):adapter.qualified_sql_semantic_transfer_installation(ctx,**kwargs)
        assert calls==[]
        return
    installation=adapter.qualified_sql_semantic_transfer_installation(ctx,**kwargs)
    assert installation.verify_runtime()==pin and installation.verify_scene is scene
    if variant=='protection_revoked':db.semantic_protection_pins=lambda owner:None
    if variant=='valid':
        installation.load_arrays.verify_inputs(params,image,image)
        assert calls==['pins','pins','origin','pins'] and len(colors)==2
    else:
        with pytest.raises(AdoptionInvalid):installation.load_arrays.verify_inputs(params,image,image)


@pytest.mark.parametrize('revoked',[False,True])
def test_sql_installer_resolves_typed_protection_origin_without_custom_verifier(tmp_path,revoked):
    from test_semantic_qualification_loader import loader_fixture
    from test_semantic_source_authority import fixture
    from photo_worker.contracts.images import MaskRef
    from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
    from photo_worker.v2.adoption import AdoptionInvalid
    db,objects,record,_=loader_fixture()
    source,colors,params,image,lease,raw,_=fixture(tmp_path)
    pin=SemanticQualificationPin.from_value(record['pin'])
    refs={'source_protection':params.intent.model_dump(mode='json'),
          'target_protection':params.calibration.model_dump(mode='json')}
    db.semantic_protection_pins=lambda owner:refs
    db.semantic_source_ready=source.semantic_source_ready
    origins=[]
    def origin(owner,binding,root,child,*,target):
        origins.append((root,child,target));return not revoked
    db.semantic_protection_origin=origin
    mask=MaskRef(artifact=params.reference.source,canvas=image.canvas,
        transform_chain=image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=True)
    installation=adapter.qualified_sql_semantic_transfer_installation(
        SimpleNamespace(check_active=lambda:None,lease=lease),database=db,objects=objects,bucket='owned',
        frozen_pin=pin,model_lock_sha=pin.model_lock_sha,runtime_lock_sha=pin.runtime_lock_sha,
        device_profile_sha=pin.device_profile_sha,dataset_sha=pin.dataset_sha,params_bytes=raw,
        verify_color=colors.append,verify_scene=lambda:None,load_protection_document=lambda ref,model:mask)
    if revoked:
        with pytest.raises(AdoptionInvalid,match='ORIGIN_STALE'):
            installation.load_arrays.verify_inputs(params,image,image)
    else:
        installation.load_arrays.verify_inputs(params,image,image)
        assert [value[0] for value in origins]==[params.intent,params.calibration]
        assert all(value[1]==mask.artifact for value in origins)


@pytest.mark.parametrize('mode',['missing','ambiguous','invalid_reader'])
def test_sql_installer_requires_unambiguous_protection_owner(mode):
    kwargs={}
    if mode=='ambiguous':kwargs=dict(verify_protection=lambda:None,load_protection_document=lambda:None)
    if mode=='invalid_reader':kwargs=dict(load_protection_document=object())
    with pytest.raises(ValueError,match='INSTALLATION_INVALID'):
        adapter.qualified_sql_semantic_transfer_installation(object(),database=object(),objects=object(),
            bucket='owned',frozen_pin=object(),model_lock_sha='a'*64,runtime_lock_sha='b'*64,
            device_profile_sha='c'*64,dataset_sha='d'*64,params_bytes=b'{}',
            verify_color=lambda:None,verify_scene=lambda:None,**kwargs)


@pytest.mark.parametrize('mode',['valid','invalid','exception'])
def test_artifact_installer_uses_live_session_and_closes_on_failure(monkeypatch,mode):
    events=[];artifacts=object();config=object()
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={'backend':'cuda','renderer_version':'v2.2'}))
    installation=adapter.SemanticTransferInstallation(lambda:None,lambda:None,lambda:None)
    @contextmanager
    def create(value,settings,roles):
        assert value is ctx and settings is config
        events.append('open')
        try:yield artifacts
        finally:events.append('close')
    def factory(value,session):
        assert value is ctx and session is artifacts
        events.append('install')
        if mode=='exception':raise ValueError('scope denied')
        return installation if mode=='valid' else None
    def execute(value,session,**kwargs):
        assert session is artifacts and kwargs['load_arrays'] is installation.load_arrays
        events.append('execute');return OperationResult([])
    monkeypatch.setattr(adapter.TaskArtifacts,'create',create)
    monkeypatch.setattr(adapter,'execute_transfer',execute)
    operation,=adapter.semantic_transfer_operations(config,artifact_installation_factory=factory)
    if mode=='valid':
        operation.execute(ctx)
        assert events==['open','install','execute','close']
    else:
        with pytest.raises((ValueError,OperationFailure)):operation.execute(ctx)
        assert events==['open','install','close']


def test_artifact_installer_rejects_two_owners():
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        adapter.semantic_transfer_operations(object(),installation_factory=lambda:None,
            artifact_installation_factory=lambda:None)


@pytest.mark.parametrize('mutation',[None,'lookup','params','after_install'])
def test_gateway_sql_factory_uses_exact_params_and_live_document_owner(tmp_path,mutation):
    from test_semantic_qualification_loader import loader_fixture
    from test_semantic_source_authority import fixture
    from photo_worker.contracts.images import MaskRef
    from photo_worker.contracts.semantic import SemanticTransferParams
    from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
    db,objects,record,_=loader_fixture()
    source,colors,params,image,lease,raw,_=fixture(tmp_path)
    pin=SemanticQualificationPin.from_value(record['pin'])
    def qualification(owner):
        if mutation=='lookup':lease.command.params.sha256='0'*64
        return record['pin']
    db.semantic_qualification=qualification
    db.semantic_source_ready=source.semantic_source_ready
    db.semantic_protection_pins=lambda owner:dict(source_protection=params.intent.model_dump(mode='json'),
        target_protection=params.calibration.model_dump(mode='json'))
    db.semantic_protection_origin=lambda *args,**kwargs:True
    mask=MaskRef(artifact=params.reference.source,canvas=image.canvas,transform_chain=image.transform_chain,
        selector='manual',confidence=1.,model_lock_id=None,protected=True)
    reads=[]
    def raw_params(ref,model):
        assert ref==lease.command.params and model is SemanticTransferParams
        reads.append('params')
        if mutation=='params':lease.command.params.sha256='0'*64
        return raw
    def document(ref,model):
        assert model is MaskRef
        reads.append(ref);return mask
    factory=adapter.sql_semantic_artifact_factory(database=db,objects=objects,bucket='owned',
        model_lock_sha=pin.model_lock_sha,runtime_lock_sha=pin.runtime_lock_sha,
        device_profile_sha=pin.device_profile_sha,dataset_sha=pin.dataset_sha,
        verify_color=colors.append,verify_scene=lambda:None)
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
    artifacts=SimpleNamespace(contract_bytes=raw_params,contract=document)
    from photo_worker.v2.adoption import AdoptionInvalid
    if mutation in ('lookup','params'):
        with pytest.raises(AdoptionInvalid,match='LEASE_CHANGED'):factory(ctx,artifacts)
        assert reads==([] if mutation=='lookup' else ['params'])
        return
    installed=factory(ctx,artifacts)
    if mutation=='after_install':
        lease.command.params.sha256='0'*64
        with pytest.raises(AdoptionInvalid,match='LEASE_CHANGED'):
            installed.load_arrays.verify_inputs(params,image,image)
        assert reads==['params']
        return
    installed.load_arrays.verify_inputs(params,image,image)
    assert reads==['params',params.intent,params.calibration]
    assert installed.verify_runtime()==pin


@pytest.mark.parametrize('invalid',['color','scene','sha','bucket','objects','database'])
def test_gateway_factory_rejects_missing_runtime_dependencies_before_install(invalid):
    names=('semantic_qualification','semantic_report_evidence','semantic_source_ready',
        'semantic_protection_pins','semantic_protection_origin')
    kwargs=dict(database=SimpleNamespace(**{name:lambda:None for name in names}),
        objects=SimpleNamespace(get_object=lambda:None),bucket='owned',model_lock_sha='a'*64,
        runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64,
        verify_color=lambda:None,verify_scene=lambda:None)
    if invalid=='color':kwargs['verify_color']=None
    elif invalid=='scene':kwargs['verify_scene']=None
    elif invalid=='sha':kwargs['device_profile_sha']='unknown'
    elif invalid=='bucket':kwargs['bucket']=''
    elif invalid=='objects':kwargs['objects']=object()
    else:kwargs['database']=object()
    with pytest.raises(ValueError,match='INSTALLATION_INVALID'):
        adapter.sql_semantic_artifact_factory(**kwargs)
