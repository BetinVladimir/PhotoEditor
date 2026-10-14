"""Two-phase V2 result adoption; object reads finish before the commit TX."""
from __future__ import annotations

from dataclasses import dataclass, replace
import random
import time
from uuid import UUID

from psycopg import Connection, Error
from psycopg.types.json import Jsonb

from .adoption import AdoptionInvalid, ArtifactRef, prepare_v2_adoption
from .adoption_store import PostgresObjectEvidenceReader, VersionedObjectStore
from .capture_adoption import prepare_capture_adoption
from .capture_prepare_adoption import prepare_capture_inputs_adoption


def _capture_rejection_code(error: Error) -> str | None:
    # Classify only explicit invariant failures, never arbitrary DB messages.
    if error.sqlstate != '23514':
        return None
    return {
        'V24_IDENTITY_CONSENT_REVOKED': 'CONSENT_REVOKED',
        'V24_CAPTURE_CONTEXT_STALE': 'CAPTURE_CONTEXT_STALE',
        'CAPTURE_CONSENT_EVIDENCE_MISSING': 'CAPTURE_CONSENT_EVIDENCE_MISSING',
    }.get(error.diag.message_primary)


def _publish_donor_review(connection, tenant_id: UUID, task_id: UUID, result_id: UUID) -> UUID:
    row = connection.execute(
        'SELECT photo.create_v28_open_eyes_review(%s,%s,%s)',
        (tenant_id, task_id, result_id),
    ).fetchone()
    if row is None or not isinstance(row[0], UUID):
        raise AdoptionInvalid('DONOR_REVIEW_PUBLICATION_STALE')
    return row[0]


@dataclass(frozen=True)
class AdoptionContext:
    attempt: int
    fence: int
    result_id: UUID
    operation: str
    version: str
    root: ArtifactRef


class AdoptionCoordinator:
    def __init__(self, read_connection: Connection, write_connection: Connection,
                 objects: VersionedObjectStore, allowed_bucket: str, *,
                 layer_scratch_root=None, layer_scratch_bytes=None,
                 region_target_icc_sha=None, render_replay=None,
                 render_max_tile_bytes=None, semantic_authority=None, semantic_ram_bytes=None,
                 semantic_plan_snapshot=None, semantic_plan_provenance=None,
                 exposure_installation=None, donor_installation=None, negative_installation=None, relight_installation=None, perspective_installation=None, perspective_lines_installation=None, multi_installations=(),
                 interchange_recipe_qualifier_factory=None, signing_operator=None, check_active=lambda: None):
        if not read_connection.autocommit or read_connection is write_connection:
            raise ValueError("Use distinct autocommit read and transactional write connections")
        if not callable(check_active):
            raise ValueError("Adoption activity check must be callable")
        if interchange_recipe_qualifier_factory is not None and not callable(interchange_recipe_qualifier_factory):
            raise ValueError('Interchange recipe qualifier factory must be callable')
        self.interchange_recipe_qualifier_factory = interchange_recipe_qualifier_factory
        if signing_operator is not None:
            from photo_worker.operations.provenance.installation import InstalledSigningOperator
            if not isinstance(signing_operator, InstalledSigningOperator):
                raise ValueError('Signing adoption requires installed operator')
        self.signing_operator = signing_operator
        def require_active():
            if check_active() is False:
                raise AdoptionInvalid('ADOPTION_INACTIVE')
        self.check_active = require_active
        self.read_connection = read_connection
        self.write_connection = write_connection
        self.objects = objects
        self.allowed_bucket = allowed_bucket
        self.layer_scratch_root = layer_scratch_root
        self.layer_scratch_bytes = layer_scratch_bytes
        if region_target_icc_sha is not None and (
                not isinstance(region_target_icc_sha, str) or len(region_target_icc_sha) != 64
                or any(c not in '0123456789abcdef' for c in region_target_icc_sha)):
            raise ValueError("Trusted region target ICC SHA must be lowercase SHA256")
        self.region_target_icc_sha = region_target_icc_sha
        if render_replay is not None and not callable(render_replay):
            raise ValueError("Render replay must be an operator-configured context factory")
        self.render_replay = render_replay
        self.render_max_tile_bytes = render_max_tile_bytes
        if semantic_authority is not None and not callable(semantic_authority):
            raise ValueError('Semantic authority must be operator configured')
        if semantic_ram_bytes is not None and (type(semantic_ram_bytes) is not int or not 1<=semantic_ram_bytes<=2**53-1):
            raise ValueError('Invalid semantic replay RAM budget')
        self.semantic_authority,self.semantic_ram_bytes=semantic_authority,semantic_ram_bytes
        for callback in (semantic_plan_snapshot, semantic_plan_provenance):
            if callback is not None and not callable(callback):
                raise ValueError('Semantic plan authority must be operator configured')
        self.semantic_plan_snapshot = semantic_plan_snapshot
        self.semantic_plan_provenance = semantic_plan_provenance
        if exposure_installation is not None and not callable(exposure_installation):
            raise ValueError('Exposure installation must be operator configured')
        self.exposure_installation = exposure_installation
        self.donor_installation = None
        if donor_installation is not None:
            from copy import deepcopy
            from pathlib import Path
            from .portrait_donor_adoption_prepare import DonorAdoptionInstallation
            from photo_worker.operations.portrait.donor_adapter import DonorRuntimeOptions
            if (not isinstance(donor_installation,DonorAdoptionInstallation)
                    or not isinstance(donor_installation.options,DonorRuntimeOptions)
                    or not isinstance(donor_installation.policy,dict)
                    or type(donor_installation.ram_max_bytes) is not int
                    or not 1<=donor_installation.ram_max_bytes<=2**53-1
                    or type(donor_installation.output_max_bytes) is not int
                    or not 1<=donor_installation.output_max_bytes<=2**53-1
                    or not isinstance(donor_installation.output_scratch_root,Path)
                    or not donor_installation.output_scratch_root.is_dir()
                    or donor_installation.output_scratch_root.is_symlink()):
                raise ValueError('Invalid donor adoption installation')
            self.donor_installation = deepcopy(donor_installation)

        self.perspective_lines_installation=None
        if perspective_lines_installation is not None:
            from .perspective_lines_adoption_prepare import validate_perspective_lines_adoption_installation
            self.perspective_lines_installation=validate_perspective_lines_adoption_installation(perspective_lines_installation)

        self.multi_installations={}
        if not isinstance(multi_installations,(list,tuple)):
            raise ValueError('Invalid multi adoption installations')
        from .multi_adoption_prepare import validate_multi_adoption_installation
        for item in multi_installations:
            owned=validate_multi_adoption_installation(item)
            operation=owned.options.operation
            if operation not in ('optical.panorama','optical.focus_stack') or operation in self.multi_installations:
                raise ValueError('Invalid duplicate multi adoption operation')
            self.multi_installations[operation]=owned

        self.perspective_installation=None
        if perspective_installation is not None:
            from .perspective_adoption_prepare import validate_perspective_adoption_installation
            self.perspective_installation=validate_perspective_adoption_installation(perspective_installation)

        self.negative_installation=None
        if negative_installation is not None:
            from copy import deepcopy
            from pathlib import Path
            from .negative_adoption_prepare import NegativeAdoptionInstallation
            from photo_worker.operations.optical.negative_adapter import NegativeRuntimeOptions
            item=negative_installation
            if (not isinstance(item,NegativeAdoptionInstallation) or not isinstance(item.options,NegativeRuntimeOptions)
                    or not isinstance(item.policy,dict) or not callable(item.verify_color)
                    or type(item.max_bytes) is not int or not 1<=item.max_bytes<=2**53-1
                    or not isinstance(item.scratch_root,Path) or item.scratch_root.is_symlink() or not item.scratch_root.is_dir()):
                raise ValueError('Invalid negative adoption installation')
            self.negative_installation=deepcopy(item)

        self.relight_installation=None
        if relight_installation is not None:
            from copy import deepcopy
            from pathlib import Path
            from .relight_adoption_prepare import RelightAdoptionInstallation
            from photo_worker.operations.portrait.relight_runtime_lock import verify_installed_runtime
            item=relight_installation
            if (not isinstance(item,RelightAdoptionInstallation)
                    or any(type(value) is not int or not 1<=value<=2**53-1 for value in (item.max_bytes,item.max_scratch_bytes))
                    or item.max_bytes>item.max_scratch_bytes
                    or not isinstance(item.scratch_root,Path) or not item.scratch_root.is_dir() or item.scratch_root.is_symlink()):
                raise ValueError('Invalid relight adoption installation')
            verify_installed_runtime(item.runtime_lock,item.runtime_sha)
            self.relight_installation=deepcopy(item)

    def context(self, tenant_id: UUID, task_id: UUID) -> AdoptionContext:
        row = self.read_connection.execute(
            "SELECT * FROM photo.v2_adoption_context(%s,%s)", (tenant_id, task_id),
        ).fetchone()
        if row is None and self.exposure_installation is not None:
            row = self.read_connection.execute(
                "SELECT * FROM photo.v27_exposure_adoption_context(%s,%s)", (tenant_id, task_id),
            ).fetchone()
        if row is None:
            raise AdoptionInvalid("V2_ADOPTION_CONTEXT_STALE")
        return AdoptionContext(row[0], row[1], row[2], row[3], row[4],
                               ArtifactRef(row[5], row[6]))

    def adopt(self, tenant_id: UUID, task_id: UUID) -> bool:
        self.check_active()
        if self.signing_operator is not None:
            from .provenance_adoption_recovery import signing_adoption_committed
            if signing_adoption_committed(self.read_connection, tenant_id, task_id, check_active=self.check_active):
                return True
        if self.perspective_lines_installation is not None:
            from .perspective_lines_adoption_recovery import perspective_lines_adoption_committed
            if perspective_lines_adoption_committed(self.read_connection,tenant_id,task_id,check_active=self.check_active):
                return True
        try:
            context = self.context(tenant_id, task_id)
        except AdoptionInvalid as error:
            if str(error) != 'V2_ADOPTION_CONTEXT_STALE':
                raise
            from .delivery_assembly_recovery import delivery_assembly_adoption_committed
            if delivery_assembly_adoption_committed(self.read_connection, tenant_id, task_id,
                                                    check_active=self.check_active):
                return True
            from .interchange_parse_adoption_recovery import interchange_parse_adoption_committed
            if interchange_parse_adoption_committed(self.read_connection, tenant_id, task_id, check_active=self.check_active):
                return True
            from .interchange_import_adoption_recovery import interchange_import_adoption_committed
            if interchange_import_adoption_committed(self.read_connection, tenant_id, task_id, check_active=self.check_active):
                return True
            raise

        if context.operation == 'v2.interchange.import':
            from .interchange_import_adoption_recovery import interchange_import_adoption_committed
            if interchange_import_adoption_committed(self.read_connection, tenant_id, task_id, check_active=self.check_active):
                return True
        if context.operation == 'v2.interchange.parse':
            from .interchange_parse_adoption_recovery import interchange_parse_adoption_committed
            if interchange_parse_adoption_committed(self.read_connection, tenant_id, task_id, check_active=self.check_active):
                return True
        self.check_active()
        if context.operation == 'region.preview' and self.region_target_icc_sha is None:
            raise AdoptionInvalid('REGION_ADOPTION_ICC_REQUIRED')
        reader = PostgresObjectEvidenceReader(
            self.read_connection, self.objects, tenant_id=tenant_id, task_id=task_id,
            attempt=context.attempt, fence=context.fence, allowed_bucket=self.allowed_bucket,
            exposure=context.operation == 'semantic.local_exposure',
        )
        render_cache_body = None
        prepared_donor = None
        prepared_negative = None
        prepared_perspective_lines = None
        prepared_perspective = None
        prepared_multi = None
        prepared_relight = None
        prepared_parse = None
        prepared_signing = None
        delivery_commit_signed = False
        if context.operation == 'render.compose':
            from .render_adoption_inputs import load_render_adoption_inputs
            from .render_prepare_adoption import prepare_render_adoption
            inputs = load_render_adoption_inputs(self.read_connection, tenant_id, task_id, context)
            from .render_qualification_pin import load_render_adoption_qualification
            inputs = replace(inputs,qualification_pin=load_render_adoption_qualification(
                self.read_connection,tenant_id,task_id,context))
            from .render_cache_identity import render_cache_identity
            inputs = replace(inputs,cache_identity=render_cache_identity(tenant_id,inputs.recipe,
                inputs.execution_profile(),inputs.parent_roots,inputs.qualification_pin))
            from .render_cache_identity import render_cache_context
            from photo_worker.contracts.base import canonical_bytes
            render_cache_body = canonical_bytes(render_cache_context(tenant_id,inputs.recipe,
                inputs.execution_profile(),inputs.parent_roots,inputs.qualification_pin)).decode('utf-8')
            prepared = prepare_render_adoption(task_id, context.root, reader, inputs,
                self.render_replay, scratch_root=self.layer_scratch_root,
                max_bytes=self.layer_scratch_bytes, max_tile_bytes=self.render_max_tile_bytes)
        elif context.operation == 'portrait.open_eyes':
            if self.donor_installation is None:
                raise AdoptionInvalid('DONOR_ADOPTION_INSTALLATION_REQUIRED')
            from .portrait_donor_adoption_prepare import prepare_donor_reconstruction
            installation=self.donor_installation
            prepared_donor=prepare_donor_reconstruction(self.read_connection,self.objects,reader,
                tenant_id,task_id,context,bucket=self.allowed_bucket,policy=installation.policy,
                options=installation.options,output_scratch_root=installation.output_scratch_root,
                output_max_bytes=installation.output_max_bytes,ram_max_bytes=installation.ram_max_bytes,check_active=self.check_active)
            prepared=prepare_v2_adoption(task_id=task_id,operation=context.operation,version=context.version,
                root=context.root,reader=reader,params_ref=prepared_donor.params_ref,check_active=self.check_active)
        elif context.operation == 'image.relight':
            if self.relight_installation is None:
                raise AdoptionInvalid('RELIGHT_ADOPTION_INSTALLATION_REQUIRED')
            row=self.read_connection.execute('SELECT * FROM photo.v28_relight_adoption_params(%s,%s,%s,%s,%s)',
                (tenant_id,task_id,context.attempt,context.fence,context.result_id)).fetchone()
            if row is None:raise AdoptionInvalid('RELIGHT_ADOPTION_PARAMS_STALE')
            from .relight_adoption_prepare import prepare_relight_reconstruction
            prepared_relight=prepare_relight_reconstruction(self.read_connection,reader,tenant_id,task_id,context,
                ArtifactRef(row[0],row[1]),installation=self.relight_installation,check_active=self.check_active)
            prepared=prepared_relight.prepared
        elif context.operation == 'optical.perspective_lines':
            if self.perspective_lines_installation is None:
                raise AdoptionInvalid('PERSPECTIVE_LINES_ADOPTION_INSTALLATION_REQUIRED')
            row=self.read_connection.execute('SELECT * FROM photo.v29_perspective_lines_adoption_params(%s,%s,%s,%s,%s)',
                (tenant_id,task_id,context.attempt,context.fence,context.result_id)).fetchone()
            if row is None:raise AdoptionInvalid('PERSPECTIVE_LINES_ADOPTION_PARAMS_STALE')
            from .perspective_lines_adoption_prepare import prepare_perspective_lines_adoption
            options=self.perspective_lines_installation
            params_ref=ArtifactRef(row[0],row[1])
            prepared_perspective_lines=prepare_perspective_lines_adoption(self.read_connection,reader,tenant_id,task_id,context,
                params_ref,installation=options.runtime,scratch_root=options.scratch_root,max_bytes=options.max_bytes,
                ram_max_bytes=options.ram_max_bytes,check_active=self.check_active)
            prepared=prepare_v2_adoption(task_id=task_id,operation=context.operation,version=context.version,
                root=context.root,reader=reader,params_ref=params_ref,check_active=self.check_active)
        elif context.operation in ('optical.panorama','optical.focus_stack'):
            installation=self.multi_installations.get(context.operation)
            if installation is None:raise AdoptionInvalid('MULTI_ADOPTION_INSTALLATION_REQUIRED')
            row=self.read_connection.execute('SELECT * FROM photo.v29_multi_adoption_params(%s,%s,%s,%s,%s)',
                (tenant_id,task_id,context.attempt,context.fence,context.result_id)).fetchone()
            if row is None:raise AdoptionInvalid('MULTI_ADOPTION_PARAMS_STALE')
            from .multi_adoption_prepare import prepare_multi_reconstruction
            prepared_multi=prepare_multi_reconstruction(self.read_connection,reader,tenant_id,task_id,context,
                ArtifactRef(row[0],row[1]),installation=installation,check_active=self.check_active)
            prepared=prepare_v2_adoption(task_id=task_id,operation=context.operation,version=context.version,
                root=context.root,reader=reader,params_ref=prepared_multi.params_ref,check_active=self.check_active)
        elif context.operation == 'optical.perspective':
            if self.perspective_installation is None:
                raise AdoptionInvalid('PERSPECTIVE_ADOPTION_INSTALLATION_REQUIRED')
            row=self.read_connection.execute('SELECT * FROM photo.v29_perspective_adoption_params(%s,%s,%s,%s,%s)',
                (tenant_id,task_id,context.attempt,context.fence,context.result_id)).fetchone()
            if row is None:raise AdoptionInvalid('PERSPECTIVE_ADOPTION_PARAMS_STALE')
            from .perspective_adoption_prepare import prepare_perspective_reconstruction
            prepared_perspective=prepare_perspective_reconstruction(self.read_connection,reader,tenant_id,task_id,context,
                ArtifactRef(row[0],row[1]),installation=self.perspective_installation,check_active=self.check_active)
            prepared=prepare_v2_adoption(task_id=task_id,operation=context.operation,version=context.version,
                root=context.root,reader=reader,params_ref=prepared_perspective.params_ref,check_active=self.check_active)
        elif context.operation == 'optical.negative':
            if self.negative_installation is None:
                raise AdoptionInvalid('NEGATIVE_ADOPTION_INSTALLATION_REQUIRED')
            row=self.read_connection.execute('SELECT * FROM photo.v29_negative_adoption_params(%s,%s,%s,%s,%s)',
                (tenant_id,task_id,context.attempt,context.fence,context.result_id)).fetchone()
            if row is None:raise AdoptionInvalid('NEGATIVE_ADOPTION_PARAMS_STALE')
            from .negative_adoption_prepare import prepare_negative_reconstruction
            installation=self.negative_installation
            prepared_negative=prepare_negative_reconstruction(self.read_connection,reader,tenant_id,task_id,context,
                ArtifactRef(row[0],row[1]),policy=installation.policy,options=installation.options,
                scratch_root=installation.scratch_root,max_bytes=installation.max_bytes,
                verify_color=installation.verify_color,check_active=self.check_active)
            prepared=prepare_v2_adoption(task_id=task_id,operation=context.operation,version=context.version,
                root=context.root,reader=reader,params_ref=prepared_negative.params_ref,check_active=self.check_active)
        elif context.operation == 'v2.interchange.import':
            from .interchange_import_adoption_prepare import prepare_current_interchange_import
            qualifier = (self.interchange_recipe_qualifier_factory(reader, tenant_id, task_id, context)
                         if self.interchange_recipe_qualifier_factory is not None else None)
            prepared_import = prepare_current_interchange_import(self.read_connection, reader,
                tenant_id, task_id, context, qualify_recipe=qualifier, check_active=self.check_active)
            prepared = prepare_v2_adoption(task_id=task_id, operation=context.operation, version=context.version,
                root=context.root, reader=reader,
                params_ref=ArtifactRef(prepared_import.params_id, prepared_import.params_sha256),
                check_active=self.check_active)
        elif context.operation == 'provenance.sign':
            if self.signing_operator is None:
                raise AdoptionInvalid('SIGNING_ADOPTION_OPERATOR_REQUIRED')
            if self.layer_scratch_root is None or self.layer_scratch_bytes is None:
                raise AdoptionInvalid('SIGNING_ADOPTION_SCRATCH_REQUIRED')
            from photo_worker.runtime.scratch import ScratchSpace
            from .provenance_adoption_prepare import prepare_current_signing
            with ScratchSpace(self.layer_scratch_root, tenant_id, task_id, context.attempt, self.layer_scratch_bytes) as scratch:
                prepared_signing = prepare_current_signing(self.read_connection, reader,
                    tenant_id, task_id, context, scratch, self.signing_operator, check_active=self.check_active)
            prepared = prepared_signing.adoption
        elif context.operation == 'v2.interchange.parse':
            if self.layer_scratch_root is None or self.layer_scratch_bytes is None:
                raise AdoptionInvalid('INTERCHANGE_ADOPTION_SCRATCH_REQUIRED')
            from photo_worker.runtime.scratch import ScratchSpace
            from .interchange_parse_adoption_prepare import prepare_current_interchange_parse
            with ScratchSpace(self.layer_scratch_root, tenant_id, task_id, context.attempt, self.layer_scratch_bytes) as scratch:
                prepared_parse = prepare_current_interchange_parse(self.read_connection, reader,
                    tenant_id, task_id, context, scratch, check_active=self.check_active)
            prepared = prepared_parse.adoption
        elif context.operation == 'v2.delivery.prepare':
            row = self.read_connection.execute(
                "SELECT * FROM photo.v30_delivery_prepare_adoption_params(%s,%s,%s,%s,%s)",
                (tenant_id, task_id, context.attempt, context.fence, context.result_id),
            ).fetchone()
            if row is None:
                raise AdoptionInvalid('DELIVERY_PREPARATION_PARAMS_STALE')
            from .delivery_prepare_adoption import verify_delivery_preparation
            delivery_params_ref = ArtifactRef(row[0], row[1])
            delivery_prepared = verify_delivery_preparation(reader, delivery_params_ref,
                context.root, check_active=self.check_active)
            prepared = prepare_v2_adoption(task_id=task_id, operation=context.operation,
                version=context.version, root=context.root, reader=reader,
                params_ref=delivery_params_ref, check_active=self.check_active)
        elif context.operation == 'v2.delivery.assemble':
            row = self.read_connection.execute(
                "SELECT * FROM photo.v30_delivery_assembly_adoption_params(%s,%s,%s,%s,%s)",
                (tenant_id, task_id, context.attempt, context.fence, context.result_id),
            ).fetchone()
            if row is None:
                raise AdoptionInvalid('DELIVERY_ASSEMBLY_PARAMS_STALE')
            delivery_params_ref = ArtifactRef(row[0], row[1])
            params_raw = reader.read_json(delivery_params_ref, 65536)
            from hashlib import sha256
            if (not isinstance(params_raw, bytes)
                    or sha256(params_raw).hexdigest() != delivery_params_ref.sha256):
                raise AdoptionInvalid('DELIVERY_ASSEMBLY_PARAMS_BYTES_INVALID')
            signed_values = (tenant_id, task_id, context.attempt, context.fence,
                             context.result_id, params_raw.decode('utf-8'))
            def signed_scope():
                self.check_active()
                scoped = self.read_connection.execute(
                    'SELECT photo.v30_delivery_signed_adoption_scope(%s,%s,%s,%s,%s,%s)',
                    signed_values).fetchone()
                return None if scoped is None else scoped[0]
            scope = signed_scope()
            if scope is None:
                from .delivery_assembly_adoption import verify_delivery_assembly
                delivery_assembled = verify_delivery_assembly(reader, delivery_params_ref,
                    context.root, check_active=self.check_active)
                signed_identity = None
            else:
                if not isinstance(scope, dict):
                    raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_SCOPE_INVALID')
                try:
                    signed_identity = tuple(UUID(str(scope[key])) for key in
                                            ('tenant_id', 'actor_id', 'parent_id'))
                except (KeyError, TypeError, ValueError) as error:
                    raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_SCOPE_INVALID') from error
                if (signed_identity[0] != tenant_id
                        or scope.get('task_id') != str(task_id)
                        or scope.get('result_id') != str(context.result_id)
                        or scope.get('params_artifact_id') != str(delivery_params_ref.artifact_id)
                        or scope.get('params_sha') != delivery_params_ref.sha256):
                    raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_SCOPE_INVALID')
                from .delivery_signed_assembly_adoption import (
                    verify_signed_delivery_assembly, verify_signed_delivery_dependency_bytes)
                from photo_worker.contracts.delivery import (V2DeliveryAssembleParams,
                    SignedDeliveryAssemblyEnvelope)
                pinned = V2DeliveryAssembleParams.model_validate_json(params_raw)
                envelope_raw = reader.read_json(ArtifactRef(
                    pinned.assembly.artifact_id, pinned.assembly.sha256), 65536)
                envelope = SignedDeliveryAssemblyEnvelope.model_validate_json(envelope_raw)
                closure_raw = reader.read_json(ArtifactRef(
                    envelope.signing_closure.artifact_id, envelope.signing_closure.sha256), 8388608)
                if (not isinstance(closure_raw, bytes)
                        or sha256(closure_raw).hexdigest() != envelope.signing_closure.sha256):
                    raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_CLOSURE_BYTES_INVALID')
                delivery_assembled = verify_signed_delivery_assembly(reader,
                    delivery_params_ref, context.root, *signed_identity,
                    check_active=self.check_active)
                verify_signed_delivery_dependency_bytes(reader, closure_raw,
                    check_active=self.check_active)
                if signed_scope() != scope:
                    raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_SCOPE_CHANGED')
                delivery_commit_signed = True
            prepared = prepare_v2_adoption(task_id=task_id, operation=context.operation,
                version=context.version, root=context.root, reader=reader,
                params_ref=delivery_params_ref, delivery_signed_scope=signed_identity,
                check_active=self.check_active)
        elif context.operation == 'capture.prepare':
            row = self.read_connection.execute(
                "SELECT * FROM photo.v24_capture_prepare_adoption_params(%s,%s)", (tenant_id, task_id),
            ).fetchone()
            if row is None:
                raise AdoptionInvalid('CAPTURE_PREPARATION_PARAMS_STALE')
            prepared = prepare_capture_inputs_adoption(task_id, reader,
                ArtifactRef(row[0], row[1]), context.root, check_active=self.check_active)
        else:
            params_ref = None
            exposure = None
            if context.operation == 'semantic.local_exposure':
                from .exposure_adoption import ExposureAdoptionInstallation
                if self.exposure_installation is None:
                    raise AdoptionInvalid('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
                exposure = self.exposure_installation(tenant_id, task_id, context, reader)
                self.check_active()
                if not isinstance(exposure, ExposureAdoptionInstallation):
                    raise AdoptionInvalid('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
                params_ref = exposure.params
            layer_alpha_mode = "require_float32"
            if context.operation == 'region.preview':
                row = self.read_connection.execute(
                    'SELECT * FROM photo.v26_region_adoption_params(%s,%s,%s,%s,%s)',
                    (tenant_id, task_id, context.attempt, context.fence, context.result_id),
                ).fetchone()
                if row is None:
                    raise AdoptionInvalid('REGION_ADOPTION_PARAMS_STALE')
                params_ref = ArtifactRef(row[0], row[1])
            if context.operation == 'v2.semantic.assemble':
                if self.semantic_plan_snapshot is None or self.semantic_plan_provenance is None:
                    raise AdoptionInvalid('SEMANTIC_PLAN_AUTHORITY_REQUIRED')
                row = self.read_connection.execute(
                    'SELECT * FROM photo.v27_semantic_assembly_adoption_params(%s,%s,%s,%s,%s)',
                    (tenant_id, task_id, context.attempt, context.fence, context.result_id),
                ).fetchone()
                if row is None:
                    raise AdoptionInvalid('SEMANTIC_PLAN_PARAMS_STALE')
                params_ref = ArtifactRef(row[0], row[1])
            if context.operation == 'semantic.transfer':
                row = self.read_connection.execute(
                    'SELECT * FROM photo.v27_semantic_adoption_params(%s,%s,%s,%s,%s)',
                    (tenant_id, task_id, context.attempt, context.fence, context.result_id),
                ).fetchone()
                if row is None:
                    raise AdoptionInvalid('SEMANTIC_ADOPTION_PARAMS_STALE')
                params_ref = ArtifactRef(row[0], row[1])
                if self.semantic_authority is None:
                    raise AdoptionInvalid('SEMANTIC_REPLAY_AUTHORITY_REQUIRED')
                if self.layer_scratch_root is None or self.layer_scratch_bytes is None or self.semantic_ram_bytes is None:
                    raise AdoptionInvalid('SEMANTIC_ADOPTION_SCRATCH_REQUIRED')
                from .semantic_replay_inputs import load_semantic_protection_refs,verify_pinned_semantic_replay
                protections=load_semantic_protection_refs(self.read_connection,tenant_id,task_id,
                    context.attempt,context.fence,context.result_id)
                verify_pinned_semantic_replay(reader,params_ref,context.root,
                    source_protection_ref=protections[0],target_protection_ref=protections[1],
                    verify_authority=lambda *documents:self.semantic_authority(tenant_id,task_id,context,*documents),
                    scratch_root=self.layer_scratch_root,max_bytes=self.layer_scratch_bytes,
                    max_ram_bytes=self.semantic_ram_bytes,check_active=self.check_active)

            if context.operation == 'optical.lens':
                row = self.read_connection.execute(
                    "SELECT * FROM photo.v25_lens_adoption_params(%s,%s)", (tenant_id, task_id),
                ).fetchone()
                if row is None:
                    raise AdoptionInvalid('LENS_ADOPTION_PARAMS_STALE')
                params_ref = ArtifactRef(row[0], row[1])
            if context.operation == 'layer.compose':
                row = self.read_connection.execute(
                    'SELECT * FROM photo.v25_layer_adoption_params(%s,%s,%s,%s,%s)',
                    (tenant_id,task_id,context.attempt,context.fence,context.result_id),
                ).fetchone()
                if row is None:
                    raise AdoptionInvalid('LAYER_ADOPTION_PARAMS_STALE')
                params_ref = ArtifactRef(row[0],row[1])
                policy = self.read_connection.execute(
                    'SELECT photo.v25_layer_adoption_alpha_policy(%s,%s,%s,%s,%s)',
                    (tenant_id,task_id,context.attempt,context.fence,context.result_id),
                ).fetchone()
                if policy is None or policy[0] not in ('require_float32','normalize_uint16'):
                    raise AdoptionInvalid('LAYER_ADOPTION_ALPHA_POLICY_STALE')
                layer_alpha_mode = policy[0]
            prepared = prepare_v2_adoption(task_id=task_id, operation=context.operation,
                                           version=context.version, root=context.root, reader=reader, params_ref=params_ref,
                                           layer_scratch_root=self.layer_scratch_root,
                                           layer_scratch_bytes=self.layer_scratch_bytes,layer_alpha_mode=layer_alpha_mode,
                                           region_target_icc_sha=self.region_target_icc_sha,
                                           semantic_plan_snapshot=(None if self.semantic_plan_snapshot is None else
                                               lambda evidence,intent:self.semantic_plan_snapshot(tenant_id,task_id,context,evidence,intent)),
                                           semantic_plan_provenance=(None if self.semantic_plan_provenance is None else
                                               lambda *docs:self.semantic_plan_provenance(tenant_id,task_id,context,*docs)),
                                           exposure_protection=None if exposure is None else exposure.protection,
                                           exposure_admission=None if exposure is None else exposure.verify_admission,
                                           exposure_origin=None if exposure is None else exposure.verify_origin,
                                           exposure_color=None if exposure is None else exposure.verify_color,
                                           check_active=self.check_active)
        # The SQL function locks tenant → run → task, then rechecks every
        # relation. Any cancel, revoke or fence change during preparation
        # returns false and rolls back all links/markers together.
        params = (tenant_id, task_id, context.attempt, context.fence, context.result_id,
                  Jsonb(list(prepared.inventory)), prepared.digest)
        statement = "SELECT photo.adopt_v2_plan(%s,%s,%s,%s,%s,%s,%s)"
        if prepared_donor is not None:
            params += (prepared_donor.evidence_jcs.decode('utf-8'),)
            statement = 'SELECT photo.adopt_v28_donor(%s,%s,%s,%s,%s,%s,%s,%s)'
        if prepared_relight is not None:
            params += (prepared_relight.evidence_jcs.decode('utf-8'),*prepared_relight.bodies)
            statement = 'SELECT photo.adopt_v28_relight('+','.join(['%s']*14)+')'
        if prepared_perspective_lines is not None:
            from .perspective_lines_adoption_commit import perspective_lines_commit_statement
            statement,params=perspective_lines_commit_statement(tenant_id,task_id,context,
                Jsonb(list(prepared.inventory)),prepared.digest,prepared_perspective_lines)
        if prepared_multi is not None:
            params += (prepared_multi.evidence_jcs.decode('utf-8'),prepared_multi.params_raw.decode('utf-8'))
            statement = 'SELECT photo.adopt_v29_multi(%s,%s,%s,%s,%s,%s,%s,%s,%s)'
        if prepared_perspective is not None:
            params += (prepared_perspective.evidence_jcs.decode('utf-8'),prepared_perspective.params_raw.decode('utf-8'))
            statement = 'SELECT photo.adopt_v29_perspective(%s,%s,%s,%s,%s,%s,%s,%s,%s)'
        if prepared_negative is not None:
            params += (prepared_negative.evidence_jcs.decode('utf-8'),prepared_negative.params_raw.decode('utf-8'))
            statement = 'SELECT photo.adopt_v29_negative(%s,%s,%s,%s,%s,%s,%s,%s,%s)'
        if context.operation == 'semantic.local_exposure':
            from hashlib import sha256
            from photo_worker.contracts.base import parse_json, canonical_bytes
            from photo_worker.contracts.images import MaskRef
            raw = reader.read_json(exposure.params, 1048576)
            protection_ref = ArtifactRef(exposure.protection.artifact_id, exposure.protection.sha256)
            protection_raw = reader.read_json(protection_ref, 1048576)
            self.check_active()
            if (not isinstance(raw, bytes) or sha256(raw).hexdigest() != exposure.params.sha256
                    or not isinstance(protection_raw, bytes)
                    or sha256(protection_raw).hexdigest() != protection_ref.sha256):
                raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
            protection = MaskRef.model_validate_json(canonical_bytes(parse_json(protection_raw)))
            params += (exposure.params.artifact_id, exposure.params.sha256, raw.decode('utf-8'),
                       protection_ref.artifact_id, protection_ref.sha256,
                       protection.artifact.artifact_id, protection.artifact.sha256)
            statement = "SELECT photo.adopt_semantic_exposure(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)"
        if prepared_parse is not None:
            from .interchange_parse_adoption_commit import interchange_parse_commit_statement
            statement, params = interchange_parse_commit_statement(tenant_id, task_id, context, prepared_parse)
        if prepared_signing is not None:
            from .provenance_adoption_commit import signing_commit_statement
            statement, params = signing_commit_statement(tenant_id, task_id, context, prepared_signing)
        if context.operation == 'v2.delivery.prepare':
            params += (delivery_prepared.params_jcs.decode('utf-8'),
                       delivery_prepared.result_jcs.decode('utf-8'))
            statement = "SELECT photo.adopt_v30_delivery_prepared(%s,%s,%s,%s,%s,%s,%s,%s,%s)"
        if context.operation == 'v2.delivery.assemble':
            params += (delivery_assembled.params_jcs.decode('utf-8'),
                       delivery_assembled.result_jcs.decode('utf-8'))
            statement = ("SELECT photo.adopt_v30_delivery_signed_assembled(%s,%s,%s,%s,%s,%s,%s,%s,%s)"
                         if delivery_commit_signed else
                         "SELECT photo.adopt_v30_delivery_assembled(%s,%s,%s,%s,%s,%s,%s,%s,%s)")
        if context.operation == 'capture.prepare':
            statement = "SELECT photo.adopt_capture_prepared(%s,%s,%s,%s,%s,%s,%s)"
        if context.operation == 'capture.assess':
            row = self.read_connection.execute(
                "SELECT * FROM photo.v24_capture_adoption_params(%s,%s)", (tenant_id, task_id),
            ).fetchone()
            if row is None:
                raise AdoptionInvalid('CAPTURE_ADOPTION_PARAMS_STALE')
            capture = prepare_capture_adoption(reader, ArtifactRef(row[0], row[1]), context.root)
            params += (capture.params_jcs.decode('utf-8'), capture.context_jcs.decode('utf-8'),
                       capture.report_jcs.decode('utf-8'))
            statement = "SELECT photo.adopt_capture_report(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)"
        for attempt in range(3):
            self.check_active()
            try:
                with self.write_connection.transaction():
                    self.check_active()
                    if prepared_parse is not None or prepared_donor is not None:
                        self.write_connection.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant_id),))
                    row = self.write_connection.execute(
                        statement, params,
                    ).fetchone()
                    if prepared_donor is not None and row and row[0]:
                        _publish_donor_review(self.write_connection,tenant_id,task_id,context.result_id)
                    if render_cache_body is not None:
                        self.write_connection.execute(
                            'SELECT photo.publish_v26_render_cache(%s,%s,%s,%s,%s,%s,%s,%s)',
                            (tenant_id,task_id,context.attempt,context.fence,context.result_id,
                             inputs.cache_identity,render_cache_body,Jsonb(list(prepared.inventory))),
                        ).fetchone()
                    self.check_active()
                    return bool(row and row[0])
            except Error as exc:
                if context.operation == 'capture.assess':
                    rejection = _capture_rejection_code(exc)
                    if rejection is not None:
                        raise AdoptionInvalid(rejection) from exc
                if (context.operation == 'optical.lens' and exc.sqlstate == '23514'
                        and exc.diag.message_primary == 'V25_LENS_SCOPE_STALE'):
                    raise AdoptionInvalid('LENS_SCOPE_STALE') from exc
                if exc.sqlstate not in {"40P01", "40001"}:
                    raise
                if attempt == 2:
                    raise AdoptionInvalid("RETRYABLE_CONFLICT") from exc
                time.sleep(random.uniform(0.025, 0.1))
        raise AssertionError("Unreachable adoption retry state")
