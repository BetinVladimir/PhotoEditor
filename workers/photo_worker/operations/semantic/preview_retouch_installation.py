"""Own SQL-backed retouch executor installer; operation registration is separate."""
from types import SimpleNamespace
from dataclasses import dataclass
from copy import deepcopy
from hashlib import sha256
from uuid import uuid5
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.retouch_qualification_pin import RetouchQualificationPin
from photo_worker.v2.retouch_qualification_loader import retouch_qualification_loader
from photo_worker.v2.semantic_qualification_pin import _sha
from .exposure_color import local_exposure_icc_verifier
from .preview_adapter import preview_inputs_declared
from .preview_retouch import execute_artifact_retouch_preview
from .preview_retouch_inputs import retouch_declared_inputs


@dataclass(frozen=True)
class RetouchPreviewInstallation:
    execute: object
    params_raw: bytes
    verify_admission: object
    verify_runtime: object
    verify_protection_origin: object
    verify_color: object
    check_active: object

    def __call__(self):
        return self.execute()


def sql_retouch_preview_factory(*, database, objects, bucket, icc_registry,
        model_lock_sha, runtime_lock_sha, device_profile_sha, dataset_sha, max_ram_bytes):
    expected = (model_lock_sha, runtime_lock_sha, device_profile_sha, dataset_sha)
    methods = ('admit_semantic_retouch_preview', 'retouch_preview_report_evidence',
               'retouch_preview_landmark_origin', 'retouch_preview_protection_origin')
    if (any(not _sha(value) for value in expected)
            or any(not callable(getattr(database, name, None)) for name in methods)
            or not callable(getattr(objects, 'get_object', None))
            or not isinstance(bucket, str) or not 3 <= len(bucket) <= 63
            or type(max_ram_bytes) is not int or not 1 <= max_ram_bytes <= 9007199254740991):
        raise ValueError('SEMANTIC_RETOUCH_INSTALLATION_REQUIRED')
    verify_color = local_exposure_icc_verifier(icc_registry)

    def install(ctx, artifacts):
        lease = deepcopy(ctx.lease)
        command = lease.command
        profile = deepcopy(lease.execution_profile)

        def active():
            ctx.check_active()
            if ctx.lease != lease:
                raise AdoptionInvalid('SEMANTIC_RETOUCH_LEASE_CHANGED')

        active()
        if (command.operation != 'semantic.preview_retouch_region'
                or command.operation_version != '1' or command.resource_class != 'cpu'
                or profile.get('backend') != 'cpu' or profile.get('renderer_version') != 'v2.2'):
            raise AdoptionInvalid('SEMANTIC_PREVIEW_TASK_INVALID')
        pin = RetouchQualificationPin.from_value(profile.get('qualification'))
        if ((pin.model_lock_sha, pin.runtime_lock_sha, pin.device_profile_sha, pin.dataset_sha) != expected
                or tuple(profile.get(key) for key in ('model_lock_sha', 'runtime_lock_sha',
                    'device_profile_sha', 'dataset_sha')) != expected
                or profile.get('model_lock_id') != model_lock_sha):
            raise AdoptionInvalid('SEMANTIC_RUNTIME_QUALIFICATION_CHANGED')
        resources = profile.get('resource_estimate', {})
        budget = resources.get('ram_bytes')
        if (type(budget) is not int or not 1 <= budget <= max_ram_bytes
                or type(resources.get('vram_bytes')) is not int or resources['vram_bytes'] != 0):
            raise AdoptionInvalid('SEMANTIC_RETOUCH_RESOURCE_INVALID')
        context_ref = ArtifactRef.model_validate_json(canonical_bytes(profile.get('retouch_context')))
        inputs = list(retouch_declared_inputs(artifacts, check_active=active))
        if (not isinstance(inputs, list) or not 1 <= len(inputs) <= 512
                or any(not isinstance(ref, ArtifactRef) or ref.artifact_id.int == 0 for ref in inputs)
                or len({ref.artifact_id for ref in inputs}) != len(inputs)
                or command.params not in inputs or context_ref not in inputs):
            raise AdoptionInvalid('SEMANTIC_RETOUCH_UNDECLARED_INPUT')
        raw = artifacts.contract_bytes(deepcopy(command.params), SemanticRetouchPreviewInput)
        active()
        if not isinstance(raw, bytes) or not 2 <= len(raw) <= 1048576:
            raise AdoptionInvalid('SEMANTIC_PREVIEW_BYTES_INVALID')
        params = SemanticRetouchPreviewInput.model_validate_json(raw)
        if canonical_bytes(params) != raw or sha256(raw).hexdigest() != command.params.sha256:
            raise AdoptionInvalid('SEMANTIC_PREVIEW_BYTES_INVALID')
        if (params.tenant_id != command.tenant_id
                or command.task_id != uuid5(command.run_id, 'semantic.preview_retouch_region:' + str(params.target_id))
                or (params.model_lock_sha, params.runtime_lock_sha, params.device_profile_sha) != expected[:3]
                or not preview_inputs_declared(SimpleNamespace(inputs=inputs), params)):
            raise AdoptionInvalid('SEMANTIC_PREVIEW_SCOPE_INVALID')

        def admit(owner, value):
            active()
            if owner != lease or canonical_bytes(value) != raw:
                return False
            accepted = database.admit_semantic_retouch_preview(deepcopy(lease), raw) is True
            active()
            return accepted

        def qualification(owner, value, ref, body):
            if not admit(owner, value) or ref != context_ref:
                return False
            load = retouch_qualification_loader(database, lease, objects, bucket,
                params=params, context_ref=context_ref, context_raw=body,
                frozen_pin=pin, check_active=active, declared_inputs=tuple(inputs))
            load()
            return admit(owner, value)

        def face_origin(owner, ref, source):
            if source != params.source or not admit(owner, params):
                return False
            accepted = database.retouch_preview_landmark_origin(deepcopy(lease), raw, deepcopy(ref)) is True
            return admit(owner, params) and accepted

        def protection_origin(source, root, binary):
            if source != params.source or root != params.protection or not admit(lease, params):
                return False
            accepted = database.retouch_preview_protection_origin(
                deepcopy(lease), raw, deepcopy(root), deepcopy(binary)) is True
            return admit(lease, params) and accepted

        if not admit(lease, params):
            raise AdoptionInvalid('SEMANTIC_PREVIEW_ADMISSION_DENIED')

        def execute():
            active()
            return execute_artifact_retouch_preview(ctx, artifacts,
                SemanticRetouchPreviewInput.model_validate_json(raw), deepcopy(context_ref),
                verify_admission=admit, verify_qualification=qualification,
                verify_face_origin=face_origin, verify_protection_origin=protection_origin,
                verify_color=verify_color, max_ram_bytes=budget)
        from photo_worker.contracts.semantic_retouch import RetouchPreviewAdapterContext
        active()
        context_raw = artifacts.contract_bytes(deepcopy(context_ref), RetouchPreviewAdapterContext)
        active()
        def runtime():
            return qualification(deepcopy(lease), params.model_copy(deep=True),
                deepcopy(context_ref), context_raw)
        def admission(value):
            return admit(deepcopy(lease), value)
        if runtime() is not True:
            raise AdoptionInvalid('SEMANTIC_RETOUCH_QUALIFICATION_DENIED')
        return RetouchPreviewInstallation(execute, bytes(raw), admission, runtime,
            protection_origin, verify_color, active)
    return install
