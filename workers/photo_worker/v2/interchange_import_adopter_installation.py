"""Process-owned import recipe qualifier for scheduler result adoption.

The operator supplies the runtime/consent/occurrence authorities. Archived
documents can select neither a callback nor schemas, ICC or scratch policy.
"""

from contextlib import contextmanager
from copy import deepcopy
from hashlib import sha256
from pathlib import Path
from uuid import UUID, uuid5

from photo_worker.contracts.base import ArtifactRef as WireRef, canonical_bytes, parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.scratch import ScratchSpace
from .adoption import AdoptionInvalid, ArtifactRef as EvidenceRef
from .interchange_import_adoption_inputs import read_import_adoption_input_inventory
from .interchange_import_adoption_mask_confirmation import SchedulerImportPersonMaskConfirmation
from .interchange_plan import read_executable_import_params, read_interchange_plan
from .interchange_operator_schemas import build_interchange_operator_schemas
from .interchange_recipe_semantics import build_prepared_import_recipe_qualifier


class SchedulerImportRecipeAdoptionInstallation:
    def __init__(self, connection, *, schemas_jcs, schemas_sha, work_icc_sha,
                 scratch_root, scratch_bytes, max_file_bytes, qualify_runtime,
                 check_active, extra_validators=None, qualify_confirmation=None,
                 qualify_occurrence=None, operation_schemas=None,
                 background_mappings=None):
        if (not connection.autocommit or not isinstance(schemas_jcs, bytes)
                or not 2 <= len(schemas_jcs) <= 16 << 20
                or sha256(schemas_jcs).hexdigest() != schemas_sha
                or not isinstance(work_icc_sha, str) or len(work_icc_sha) != 64
                or any(c not in '0123456789abcdef' for c in work_icc_sha)
                or not isinstance(scratch_root, Path) or not scratch_root.is_absolute()
                or scratch_root.is_symlink() or not scratch_root.is_dir()
                or type(scratch_bytes) is not int or not 1 <= scratch_bytes <= 2**53-1
                or type(max_file_bytes) is not int or not 1 <= max_file_bytes <= 128 << 30
                or not callable(qualify_runtime) or not callable(check_active)
                or qualify_confirmation is not None and not callable(qualify_confirmation)
                or qualify_occurrence is not None and not callable(qualify_occurrence)
                or background_mappings is not None and not callable(background_mappings)):
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_INSTALLATION')
        self.connection = connection
        self.schemas_jcs, self.schemas_sha = bytes(schemas_jcs), schemas_sha
        self.work_icc_sha = work_icc_sha
        self.scratch_root, self.scratch_bytes = scratch_root, scratch_bytes
        self.max_file_bytes = max_file_bytes
        definitions = parse_json(self.schemas_jcs, max_bytes=16 << 20)
        if not isinstance(definitions, dict):
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_SCHEMAS')
        build_interchange_operator_schemas(definitions.get('$defs'), operation_schemas)
        self.runtime, self.active = qualify_runtime, check_active
        self.extra_validators = dict(extra_validators or {})
        self.confirmation, self.occurrence = qualify_confirmation, qualify_occurrence
        self.operation_schemas = None if operation_schemas is None else dict(operation_schemas)
        self.background_mappings = background_mappings
        if any(not callable(callback) for callback in self.extra_validators.values()):
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_VALIDATORS')

    @contextmanager
    def __call__(self, reader, tenant_id, task_id, context):
        if (not isinstance(tenant_id, UUID) or tenant_id.int == 0
                or not isinstance(task_id, UUID) or task_id.int == 0
                or context.operation != 'v2.interchange.import' or context.version != '1'
                or type(context.attempt) is not int or context.attempt < 1
                or type(context.fence) is not int or context.fence < 1
                or not isinstance(context.result_id, UUID) or context.result_id.int == 0
                or not callable(getattr(reader, 'read_json', None))
                or not callable(getattr(reader, 'iter_bytes', None))):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_CONTEXT')
        scope = (tenant_id, task_id, context.attempt, context.fence, context.result_id)

        def current():
            self.active()
            row = self.connection.execute(
                'SELECT photo.v30_interchange_import_adoption_context(%s,%s,%s,%s,%s)',
                scope).fetchone()
            self.active()
            if row is None or not isinstance(row[0], dict) or set(row[0]) != {
                    'params', 'workflow_id', 'project_id', 'snapshot_digest',
                    'max_output_bytes'}:
                raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_AUTHORITY')
            pin = self.connection.execute(
                'SELECT photo.v30_import_adoption_schema_pin(%s,%s,%s,%s,%s)',
                scope).fetchone()
            self.active()
            if pin != (self.schemas_sha,):
                raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_SCHEMA_PIN')
            return deepcopy(row[0])

        authority = current()
        try:
            params_ref = WireRef.model_validate(authority['params'])
            workflow = UUID(str(authority['workflow_id']))
            project = UUID(str(authority['project_id']))
            snapshot_digest = authority['snapshot_digest']
            budget = authority['max_output_bytes']
            if (workflow.int == 0 or project.int == 0
                    or task_id != uuid5(workflow, 'v2.interchange.import:0')
                    or type(budget) is not int or not 1 <= budget <= 2**53-1
                    or not isinstance(snapshot_digest, str)
                    or len(snapshot_digest) != 64
                    or any(c not in '0123456789abcdef' for c in snapshot_digest)):
                raise ValueError('invalid authority')
        except (TypeError, ValueError, KeyError) as error:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_PLAN') from error
        inventory = read_import_adoption_input_inventory(
            self.connection, reader, *scope)
        indexed = {entry[0].artifact_id: entry for entry in inventory.entries}
        read_total = 0

        def read_document(reference, maximum, contract=None):
            nonlocal read_total
            self.active()
            reference = WireRef.model_validate(reference)
            entry = indexed.get(reference.artifact_id)
            if (entry is None or entry[0] != reference or entry[2] != 'application/json'
                    or contract is not None and entry[1] != contract
                    or type(maximum) is not int or maximum < 2):
                raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_INPUT_SCOPE')
            remaining = (128 << 20) - read_total
            if remaining < 2:
                raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_READ_BUDGET')
            bound = min(maximum, 8 << 20, remaining)
            raw = reader.read_json(EvidenceRef(reference.artifact_id, reference.sha256), bound)
            self.active()
            if (not isinstance(raw, bytes) or not 2 <= len(raw) <= bound
                    or sha256(raw).hexdigest() != reference.sha256):
                raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_INPUT_HASH')
            read_total += len(raw)
            return raw

        try:
            params = read_executable_import_params(params_ref,
                read_document(params_ref, 65536, 'Params'), task_id=task_id)
            plan = read_interchange_plan(read_document(params.plan, 8 << 20, 'InterchangePlan'),
                project_id=project, direction='import', snapshot_digest=snapshot_digest)
        except (TypeError, ValueError, KeyError) as error:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_PLAN') from error
        images = []
        bindings, loaded = {}, set()
        for entry in plan.entries:
            binding = entry.binding
            if binding is None:
                continue
            previous = bindings.setdefault(binding.asset_version_id, binding)
            if previous != binding:
                raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_SOURCE_CONFLICT')
            if binding.asset_version_id in loaded:
                continue
            loaded.add(binding.asset_version_id)
            raw = read_document(binding.source, 1 << 20, 'WorkingImage')
            image = WorkingImage.model_validate_json(raw)
            if canonical_bytes(image) != raw:
                raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_IMAGE_CANONICAL')
            images.append((binding, image))

        from .interchange_import_adoption_runtime import SchedulerImportAdoptionRuntimeAuthority
        bound_runtime, runtime_recheck = (self.runtime.bound_scope(scope, context)
            if type(self.runtime) is SchedulerImportAdoptionRuntimeAuthority
            else (None, None))

        def runtime(contract, typed, binding):
            self.active()
            accepted = (bound_runtime(contract, typed, binding)
                        if bound_runtime is not None else
                        self.runtime(scope, context, contract, typed, binding))
            self.active()
            return accepted

        mask_confirmation = SchedulerImportPersonMaskConfirmation(
            self.connection, scope, self.active, self.confirmation)
        def confirmation(mask, binding):
            return mask_confirmation(context, mask, binding)

        def occurrence(value):
            if self.occurrence is None:
                return False
            self.active()
            accepted = self.occurrence(context, value)
            self.active()
            return accepted

        background = None
        if self.background_mappings is not None:
            background = self.background_mappings(context, plan, inventory.entries)
        with ScratchSpace(self.scratch_root, tenant_id, task_id, context.attempt,
                          self.scratch_bytes) as scratch:
            qualifier = build_prepared_import_recipe_qualifier(
                canonical_images=images, work_icc_sha=self.work_icc_sha,
                entries=inventory.entries, schemas_jcs=self.schemas_jcs,
                schemas_sha=self.schemas_sha,
                read_document=lambda ref, maximum: read_document(ref, maximum),
                evidence_reader=reader, scratch=scratch,
                max_file_bytes=self.max_file_bytes, qualify_runtime=runtime,
                extra_validators=self.extra_validators, check_active=self.active,
                recheck_inputs=inventory.recheck,
                qualify_occurrence=occurrence if self.occurrence is not None else None,
                operation_schemas=self.operation_schemas,
                qualify_confirmation=confirmation,
                background_mappings=background)
            try:
                yield qualifier
            finally:
                if runtime_recheck is not None:
                    runtime_recheck()
                inventory.recheck()
                if current() != authority:
                    raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTER_AUTHORITY_CHANGED')
