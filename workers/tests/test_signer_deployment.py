from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.signer_deployment import load_signer_deployment
from photo_worker.runtime.signer_preflight import verify_signer_deployment_files


def fixture(tmp_path):
    root = tmp_path.resolve()
    for name, data in [('chain.pem', b'certificate'), ('trust.pem', b'trust'), ('pin', b'1234')]:
        (root/name).write_bytes(data)
        (root/name).chmod(0o600)
    policy = dict(schema_version=1, id=str(uuid4()), revision='1', sdk_lock_sha='a'*64,
        key_handle='key', certificate_chain_sha=sha256(b'certificate').hexdigest(),
        timestamp_mode='off', timestamp_service_id=None, allowed_mime=['image/png'],
        trust_bundle_sha=sha256(b'trust').hexdigest(), revocation_mode='off')
    manifest = dict(schema_version='signer-deployment/v1', sdk_lock_sha='a'*64,
        qualification_sha='d'*64, policies=[policy],
        certificate_chain=dict(path='chain.pem',sha256=policy['certificate_chain_sha']),
        trust_bundle=dict(path='trust.pem',sha256=policy['trust_bundle_sha']), algorithm='ED25519',
        key_handle='key', token_serial_hex='73657269616c', key_id_hex='01', pin_file='pin',
        timestamp_services={}, network_hosts=[])
    return root, manifest


def save(root, manifest):
    path = root/'installation.json'
    path.write_bytes(canonical_bytes(manifest))
    path.chmod(0o600)


def test_offline_signer_preflight_binds_runtime_files_without_reading_pin(tmp_path):
    from datetime import datetime, timedelta, timezone
    from cryptography import x509
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric import ed25519
    from cryptography.x509.oid import NameOID
    root, manifest = fixture(tmp_path)
    key = ed25519.Ed25519PrivateKey.generate()
    now = datetime.now(timezone.utc)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'signer preflight fixture')])
    certificate = (x509.CertificateBuilder().subject_name(name).issuer_name(name)
        .public_key(key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now-timedelta(hours=1)).not_valid_after(now+timedelta(hours=1))
        .add_extension(x509.BasicConstraints(ca=False,path_length=None),critical=True)
        .add_extension(x509.KeyUsage(digital_signature=True,content_commitment=False,
            key_encipherment=False,data_encipherment=False,key_agreement=False,
            key_cert_sign=False,crl_sign=False,encipher_only=False,decipher_only=False),critical=True)
        .sign(key,None).public_bytes(serialization.Encoding.PEM))
    (root/'chain.pem').write_bytes(certificate)
    (root/'chain.pem').chmod(0o600)
    manifest['certificate_chain']['sha256'] = sha256(certificate).hexdigest()
    manifest['policies'][0]['certificate_chain_sha'] = sha256(certificate).hexdigest()
    native = root/'native.so'
    native.write_bytes(b'qualified-binary-candidate')
    native.chmod(0o600)
    lock = {'schema_version':'signer-runtime-files/v1','files':[
        {'name':'native','sha256':sha256(native.read_bytes()).hexdigest(),
         'size_bytes':native.stat().st_size}]}
    sdk_sha = sha256(canonical_bytes(lock)).hexdigest()
    manifest['sdk_lock_sha'] = sdk_sha
    manifest['policies'][0]['sdk_lock_sha'] = sdk_sha
    save(root, manifest)
    lock_file = root/'runtime-lock.json'
    lock_file.write_bytes(canonical_bytes(lock))
    lock_file.chmod(0o600)
    (root/'pin').unlink()
    receipt = verify_signer_deployment_files(root, 'runtime-lock.json', {'native':native})
    assert receipt['valid'] is True and receipt['native_qualified'] is False
    assert receipt['sdk_lock_sha'] == sdk_sha and receipt['policy_count'] == 1
    with pytest.raises(ValueError):
        verify_signer_deployment_files(root, 'runtime-lock.json', {})
    native.write_bytes(b'altered-binary-candidate')
    with pytest.raises(ValueError):
        verify_signer_deployment_files(root, 'runtime-lock.json', {'native':native})
    native.write_bytes(b'qualified-binary-candidate')
    invalid_certificate = (x509.CertificateBuilder().subject_name(name).issuer_name(name)
        .public_key(key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now-timedelta(hours=1)).not_valid_after(now+timedelta(hours=1))
        .add_extension(x509.BasicConstraints(ca=False,path_length=None),critical=True)
        .sign(key,None).public_bytes(serialization.Encoding.PEM))
    (root/'chain.pem').write_bytes(invalid_certificate)
    manifest['certificate_chain']['sha256'] = sha256(invalid_certificate).hexdigest()
    manifest['policies'][0]['certificate_chain_sha'] = sha256(invalid_certificate).hexdigest()
    save(root,manifest)
    with pytest.raises(ValueError):
        verify_signer_deployment_files(root, 'runtime-lock.json', {'native':native})


def test_loaded_bytes_are_owned_pin_is_lazy_and_rotation_is_live(tmp_path):
    root, manifest = fixture(tmp_path)
    save(root, manifest)
    (root/'pin').unlink()
    deployment = load_signer_deployment(root)
    with pytest.raises(OSError): deployment.pin_supplier()
    (root/'pin').write_bytes(b'new-pin\n')
    (root/'pin').chmod(0o600)
    assert deployment.pin_supplier() == 'new-pin\n'
    assert deployment.token_serial == b'serial' and deployment.key_id == b'\x01'
    manifest['policies'][0]['key_handle'] = 'changed'
    assert b'changed' not in deployment.policies[0]


@pytest.mark.parametrize('mode', ['unknown','policy_pin','duplicate_policy','file_pin','traversal',
                                 'absolute','algorithm','hex','timestamp','network','missing_timestamp'])
def test_bad_configuration_fails(tmp_path, mode):
    root, manifest = fixture(tmp_path)
    if mode == 'unknown': manifest['private_key'] = 'forbidden'
    elif mode == 'policy_pin': manifest['policies'][0]['trust_bundle_sha'] = 'f'*64
    elif mode == 'duplicate_policy': manifest['policies'] *= 2
    elif mode == 'file_pin': manifest['trust_bundle']['sha256'] = 'f'*64
    elif mode == 'traversal': manifest['certificate_chain']['path'] = '../chain.pem'
    elif mode == 'absolute': manifest['pin_file'] = str(root/'pin')
    elif mode == 'algorithm': manifest['algorithm'] = 'Ed25519'
    elif mode == 'hex': manifest['key_id_hex'] = '0X01'
    elif mode == 'timestamp': manifest['timestamp_services'] = {'tsa':'http://tsa.example'}
    elif mode == 'network': manifest['network_hosts'] = ['tsa.example','tsa.example']
    elif mode == 'missing_timestamp':
        manifest['policies'][0].update(timestamp_mode='required',timestamp_service_id='tsa')
    save(root, manifest)
    with pytest.raises(ValueError): load_signer_deployment(root)


def test_deployment_owner_policy_rejects_foreign_uid():
    from photo_worker.runtime.signer_deployment import _trusted_owner
    assert _trusted_owner(0, 1001)
    assert _trusted_owner(1001, 1001)
    assert not _trusted_owner(1002, 1001)
    assert not _trusted_owner(1002, 0)


@pytest.mark.parametrize('mode', ['symlink','hardlink','writable','nested_symlink'])
def test_unsafe_files_rejected(tmp_path, mode):
    root, manifest = fixture(tmp_path)
    path = root/'chain.pem'
    if mode == 'symlink':
        path.rename(root/'real')
        path.symlink_to(root/'real')
    elif mode == 'hardlink': (root/'alias').hardlink_to(path)
    elif mode == 'writable': path.chmod(0o666)
    elif mode == 'nested_symlink':
        (root/'aliasdir').symlink_to(root,target_is_directory=True)
        manifest['certificate_chain']['path'] = 'aliasdir/chain.pem'
    save(root, manifest)
    with pytest.raises((ValueError, OSError)): load_signer_deployment(root)


def test_pin_permissions_are_stricter_than_public_files(tmp_path):
    root, manifest = fixture(tmp_path)
    save(root, manifest)
    deployment = load_signer_deployment(root)
    (root/'pin').chmod(0o644)
    with pytest.raises(ValueError, match='SIGNER_DEPLOYMENT_FILE'): deployment.pin_supplier()


def test_explicit_factory_passes_installed_pins_without_reading_pin(tmp_path, monkeypatch):
    import photo_worker.runtime.signer_installation as installation
    root, manifest = fixture(tmp_path)
    save(root, manifest)
    deployment = load_signer_deployment(root)
    (root/'pin').unlink()
    expected = object()
    def factory(database, worker, artifacts, policies, qualification, authority, **kwargs):
        assert policies == deployment.policies and qualification == 'd'*64
        assert kwargs['certificate_chain'] == b'certificate'
        assert kwargs['key_id'] == b'\x01' and kwargs['algorithm'] == 'ED25519'
        assert callable(kwargs['pin_supplier'])
        from photo_worker.operations.provenance.certificate_inspector import CertificateBoundInstallationInspector
        assert isinstance(kwargs['native_inspector'], CertificateBoundInstallationInspector)
        return expected
    monkeypatch.setattr(installation, 'build_pkcs11_signer_executor', factory)
    assert deployment.build_executor(object(), uuid4(), object(), sdk=object(), pkcs11=object(),
        library=object(), native_authority=lambda *a: True, native_inspector=lambda *a: True,
        verify_binding=lambda *a: True) is expected



def test_explicit_policy_revisions_can_coexist_without_implicit_latest(tmp_path):
    from photo_worker.contracts.provenance import SigningPolicy,decode_provenance
    root,manifest=fixture(tmp_path)
    older=manifest['policies'][0]
    newer={**older,'revision':'2','allowed_mime':['image/png','image/jpeg']}
    manifest['policies']=[newer,older]
    save(root,manifest)
    deployment=load_signer_deployment(root)
    decoded=[decode_provenance(raw,SigningPolicy,1<<20) for raw in deployment.policies]
    assert [(str(policy.id),policy.revision) for policy in decoded]==[(older['id'],'2'),(older['id'],'1')]
    assert deployment.policies==(canonical_bytes(newer),canonical_bytes(older))


def test_conflicting_same_policy_revision_is_rejected(tmp_path):
    root,manifest=fixture(tmp_path)
    manifest['policies'].append({**manifest['policies'][0],'allowed_mime':['image/jpeg']})
    save(root,manifest)
    with pytest.raises(ValueError,match='SIGNER_DEPLOYMENT_POLICY_PINS'):
        load_signer_deployment(root)


def test_new_policy_revision_cannot_change_installation_pins(tmp_path):
    root,manifest=fixture(tmp_path)
    manifest['policies'].append({**manifest['policies'][0],'revision':'2','sdk_lock_sha':'b'*64})
    save(root,manifest)
    with pytest.raises(ValueError,match='SIGNER_DEPLOYMENT_POLICY_PINS'):
        load_signer_deployment(root)


def test_deployment_read_refuses_cancelled_scope_before_open(tmp_path, monkeypatch):
    from photo_worker.runtime import signer_deployment as module
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    live = True
    def active():
        if not live:
            raise RuntimeError('cancelled')
    with signing_task_scope(active):
        live = False
        monkeypatch.setattr(module.os, 'open', lambda *args, **kwargs: pytest.fail('file opened'))
        with pytest.raises(RuntimeError, match='cancelled'):
            module._read(tmp_path, 'pin', 1024, secret=True)
        live = True


def test_deployment_load_refuses_cancelled_scope_before_root_resolution(tmp_path, monkeypatch):
    from photo_worker.runtime import signer_deployment as module
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    live = True
    def active():
        if not live:raise RuntimeError('cancelled')
    with signing_task_scope(active):
        live = False
        original_resolve=type(tmp_path).resolve
        def resolve(path,*args,**kwargs):
            if path==tmp_path:raise AssertionError('deployment root read')
            return original_resolve(path,*args,**kwargs)
        monkeypatch.setattr(type(tmp_path),'resolve',resolve)
        with pytest.raises(RuntimeError,match='cancelled'):
            module.load_signer_deployment(tmp_path)
        live = True


def test_pin_read_cancellation_discards_bytes_and_closes_descriptor(tmp_path, monkeypatch):
    import os
    from photo_worker.runtime import signer_deployment as module
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    root, manifest = fixture(tmp_path)
    save(root, manifest)
    deployment = load_signer_deployment(root)
    real_read = os.read
    descriptors = []
    live = True
    def active():
        if not live:
            raise RuntimeError('cancelled')
    def cancelled_read(descriptor, count):
        nonlocal live
        descriptors.append(descriptor)
        result = real_read(descriptor, count)
        live = False
        return result
    with signing_task_scope(active):
        monkeypatch.setattr(module.os, 'read', cancelled_read)
        with pytest.raises(RuntimeError, match='cancelled'):
            deployment.pin_supplier()
        live = True
    assert descriptors
    for descriptor in descriptors:
        with pytest.raises(OSError):
            os.fstat(descriptor)


def test_pin_read_cancellation_after_final_eof_discards_bytes(tmp_path, monkeypatch):
    import os
    from photo_worker.runtime import signer_deployment as module
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    root, manifest = fixture(tmp_path)
    save(root, manifest)
    deployment = load_signer_deployment(root)
    real_read = os.read
    descriptors = []
    live = True
    def active():
        if not live:
            raise RuntimeError('cancelled')
    def cancelled_eof(descriptor, count):
        nonlocal live
        result = real_read(descriptor, count)
        descriptors.append(descriptor)
        if count == 1 and result == b'':
            live = False
        return result
    with signing_task_scope(active):
        monkeypatch.setattr(module.os, 'read', cancelled_eof)
        with pytest.raises(RuntimeError, match='cancelled'):
            deployment.pin_supplier()
        live = True
    assert descriptors
    for descriptor in descriptors:
        with pytest.raises(OSError):
            os.fstat(descriptor)


@pytest.mark.parametrize('mode', [0o770, 0o707, 0o777])
def test_deployment_refuses_writable_root_directory(tmp_path, mode):
    root, manifest = fixture(tmp_path)
    save(root, manifest)
    original = root.stat().st_mode & 0o777
    try:
        root.chmod(mode)
        with pytest.raises(ValueError, match='SIGNER_DEPLOYMENT_DIRECTORY'):
            load_signer_deployment(root)
    finally:
        root.chmod(original)


def test_lazy_pin_refuses_writable_nested_directory(tmp_path):
    root, manifest = fixture(tmp_path)
    secrets = root/'secrets'
    secrets.mkdir(mode=0o700)
    (root/'pin').rename(secrets/'pin')
    manifest['pin_file'] = 'secrets/pin'
    save(root, manifest)
    deployment = load_signer_deployment(root)
    assert deployment.pin_supplier() == '1234'
    secrets.chmod(0o770)
    with pytest.raises(ValueError, match='SIGNER_DEPLOYMENT_DIRECTORY'):
        deployment.pin_supplier()


def test_rejected_directory_descriptor_is_closed(tmp_path, monkeypatch):
    import os
    from photo_worker.runtime import signer_deployment as module
    root, manifest = fixture(tmp_path)
    save(root, manifest)
    original = root.stat().st_mode & 0o777
    real_open = os.open
    opened = []
    def capture(*args, **kwargs):
        descriptor = real_open(*args, **kwargs)
        opened.append(descriptor)
        return descriptor
    monkeypatch.setattr(module.os, 'open', capture)
    try:
        root.chmod(0o777)
        with pytest.raises(ValueError, match='SIGNER_DEPLOYMENT_DIRECTORY'):
            load_signer_deployment(root)
    finally:
        root.chmod(original)
    assert opened
    for descriptor in opened:
        with pytest.raises(OSError):
            os.fstat(descriptor)


def test_deployed_certificate_composition_keeps_live_authority(tmp_path,monkeypatch):
    from datetime import datetime,timezone
    from test_provenance_certificate_inspector import certificate
    from photo_worker.contracts.provenance import SigningPolicy
    import photo_worker.runtime.signer_installation as installation
    root,manifest=fixture(tmp_path)
    chain=certificate(datetime.now(timezone.utc))
    (root/'chain.pem').write_bytes(chain)
    (root/'trust.pem').write_bytes(chain)
    pin=sha256(chain).hexdigest()
    manifest['certificate_chain']['sha256']=pin
    manifest['policies'][0]['certificate_chain_sha']=pin
    manifest['trust_bundle']['sha256']=pin
    manifest['policies'][0]['trust_bundle_sha']=pin
    save(root,manifest)
    deployment=load_signer_deployment(root)
    called=[]
    def live(policy,algorithm,actual):
        called.append((policy,algorithm,actual))
        return False
    def factory(*args,**kwargs):
        policy=SigningPolicy.model_validate_json(canonical_bytes(manifest['policies'][0]))
        assert kwargs['native_inspector'](policy,'ED25519',chain) is False
        return 'refused-live-authority'
    monkeypatch.setattr(installation,'build_pkcs11_signer_executor',factory)
    result=deployment.build_executor(object(),uuid4(),object(),sdk=object(),pkcs11=object(),
        library=object(),native_authority=lambda *args:True,native_inspector=live,
        verify_binding=lambda *args:True)
    assert result=='refused-live-authority' and len(called)==1


def test_deployed_certificate_composition_requires_live_inspector(tmp_path,monkeypatch):
    import photo_worker.runtime.signer_installation as installation
    root,manifest=fixture(tmp_path);save(root,manifest)
    deployment=load_signer_deployment(root)
    monkeypatch.setattr(installation,'build_pkcs11_signer_executor',lambda *args,**kwargs:pytest.fail('factory reached'))
    with pytest.raises(ValueError,match='SIGNER_CERTIFICATE_INSPECTOR'):
        deployment.build_executor(object(),uuid4(),object(),sdk=object(),pkcs11=object(),
            library=object(),native_authority=lambda *args:True,native_inspector=None,
            verify_binding=lambda *args:True)


def test_deployment_read_refuses_symlink_ancestor(tmp_path):
    from photo_worker.runtime.signer_deployment import _read
    tmp_path = tmp_path.resolve()
    real = tmp_path / 'real'
    real.mkdir(mode=0o700)
    installed = real / 'installed'
    installed.mkdir(mode=0o700)
    (installed / 'pin').write_bytes(b'1234')
    (installed / 'pin').chmod(0o600)
    alias = tmp_path / 'alias'
    alias.symlink_to(real, target_is_directory=True)
    with pytest.raises(OSError):
        _read(alias / 'installed', 'pin', 1024, secret=True)
    assert _read(installed, 'pin', 1024, secret=True) == b'1234'


def test_deployment_root_requires_absolute_non_parent_path(tmp_path):
    from photo_worker.runtime.signer_deployment import _root_directory
    for root in ('relative/installation', str(tmp_path / '..' / tmp_path.name)):
        with pytest.raises(ValueError, match='SIGNER_DEPLOYMENT_ROOT'):
            _root_directory(root)


def test_deployment_refuses_writable_ancestor_directory(tmp_path):
    root, manifest = fixture(tmp_path)
    save(root, manifest)
    ancestor = tmp_path.resolve()
    original = ancestor.stat().st_mode & 0o777
    try:
        ancestor.chmod(0o777)
        with pytest.raises(ValueError, match='SIGNER_DEPLOYMENT_DIRECTORY'):
            load_signer_deployment(root)
    finally:
        ancestor.chmod(original)
