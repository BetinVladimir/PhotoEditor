"""Build the prepared-recipe qualifier from admitted worker task inputs."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_input_reader import PreparedImportInputReader
from photo_worker.v2.interchange_plan import InterchangeImportParams, read_interchange_plan
from photo_worker.v2.interchange_recipe_semantics import build_prepared_import_recipe_qualifier


class PreparedImportRecipeFactory:
    def __init__(self, *, schemas_jcs, schemas_sha, work_icc_sha, qualify_runtime,
                 extra_validators=None, max_file_bytes=128 << 30, operation_schemas=None,
                 qualify_confirmation=None, historical_authority=None):
        if (not isinstance(schemas_jcs, bytes) or not 2 <= len(schemas_jcs) <= 16 << 20
                or sha256(schemas_jcs).hexdigest() != schemas_sha
                or not isinstance(work_icc_sha, str) or len(work_icc_sha) != 64
                or any(c not in '0123456789abcdef' for c in work_icc_sha)
                or not callable(qualify_runtime)
                or (qualify_confirmation is not None and not callable(qualify_confirmation))
                or type(max_file_bytes) is not int
                or not 1 <= max_file_bytes <= 128 << 30):
            raise ValueError('INTERCHANGE_FACTORY_INSTALLATION')
        self.schemas, self.schemas_sha, self.icc = bytes(schemas_jcs), schemas_sha, work_icc_sha
        from photo_worker.v2.interchange_operator_schemas import build_interchange_operator_schemas
        definitions = parse_json(self.schemas, max_bytes=16 << 20)
        if not isinstance(definitions, dict):
            raise ValueError('INTERCHANGE_FACTORY_INSTALLATION')
        build_interchange_operator_schemas(definitions.get('$defs'), operation_schemas)
        self.operation_schemas = None if operation_schemas is None else dict(operation_schemas)
        self.runtime, self.extra, self.maximum = qualify_runtime, dict(extra_validators or {}), max_file_bytes
        self.confirmation = qualify_confirmation
        if historical_authority is not None and not (callable(getattr(historical_authority, "bind_inputs", None)) or all(callable(getattr(historical_authority, name, None)) for name in ("ancestors", "qualify_ancestor", "qualify_faces"))):
            raise ValueError("INTERCHANGE_HISTORICAL_AUTHORITY_INSTALLATION")
        self.historical = historical_authority
        if any(not callable(value) for value in self.extra.values()):
            raise ValueError('INTERCHANGE_FACTORY_INSTALLATION')

    def build_with_read_budget(self, ctx, artifacts, authority, read_budget):
        return self(ctx, artifacts, authority, read_budget=read_budget)

    def __call__(self, ctx, artifacts, authority, *, read_budget=None):
        ctx.check_active()
        if ctx.lease.execution_profile.get('contracts_sha') != self.schemas_sha:
            raise AdoptionInvalid('INTERCHANGE_FACTORY_SCHEMA_PIN')
        from photo_worker.v2.interchange_read_budget import ImportReadBudget
        if read_budget is None:
            read_budget = ImportReadBudget(authority.max_input_json_bytes)
        if not isinstance(read_budget, ImportReadBudget) or read_budget.maximum != authority.max_input_json_bytes:
            raise AdoptionInvalid('INTERCHANGE_FACTORY_JSON_BUDGET')
        reader = PreparedImportInputReader(artifacts,max_bytes=self.maximum,
            check_active=ctx.check_active,read_budget=read_budget)
        def read(reference, maximum):
            ctx.check_active()
            remaining = read_budget.remaining_json_bytes
            if remaining < 2:
                raise AdoptionInvalid('INTERCHANGE_FACTORY_JSON_BUDGET')
            raw = reader.read_json(reference,min(maximum,remaining))
            ctx.check_active()
            return raw
        params = artifacts.params(InterchangeImportParams)
        if (params.evidence is None
                or sha256(canonical_bytes(params)).hexdigest() != ctx.lease.command.params.sha256):
            raise AdoptionInvalid('INTERCHANGE_FACTORY_EVIDENCE')
        def require_contract(reference, contract):
            entry = reader.entries.get((reference.artifact_id, reference.sha256))
            if entry is None or entry.contract != contract:
                raise AdoptionInvalid('INTERCHANGE_FACTORY_INPUT_CONTRACT')
        require_contract(params.plan, 'InterchangePlan')
        plan = read_interchange_plan(read(params.plan,8 << 20),project_id=authority.project_id,
            direction='import',snapshot_digest=authority.snapshot_digest)
        return self.build_for_plan(ctx, artifacts, authority, plan, reader)

    def build_for_plan(self, ctx, artifacts, authority, plan, reader, *, qualify_occurrence=None,
                       background_mappings=None):
        """Shared physical recipe qualifier for admitted import/export plans.

        Reuses the caller's exact input reader and aggregate read budget. The
        export bundle factory must still build its inventory from successful
        qualified_document_refs receipts, never from every declared input.
        """
        if qualify_occurrence is not None and not callable(qualify_occurrence):
            raise AdoptionInvalid('INTERCHANGE_FACTORY_OCCURRENCE_INSTALLATION')
        from photo_worker.v2.interchange_plan import InterchangePlan
        from photo_worker.v2.interchange_read_budget import ImportReadBudget
        ctx.check_active()
        plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
        if (ctx.lease.execution_profile.get('contracts_sha') != self.schemas_sha
                or not isinstance(reader, PreparedImportInputReader)
                or not isinstance(reader.read_budget, ImportReadBudget)
                or reader.read_budget.maximum != authority.max_input_json_bytes
                or plan.project_id != authority.project_id
                or plan.snapshot_digest != authority.snapshot_digest):
            raise AdoptionInvalid('INTERCHANGE_FACTORY_PLAN_AUTHORITY')
        def read(reference, maximum):
            ctx.check_active()
            remaining = reader.read_budget.remaining_json_bytes
            if remaining < 2:
                raise AdoptionInvalid('INTERCHANGE_FACTORY_JSON_BUDGET')
            raw = reader.read_json(reference, min(maximum, remaining))
            ctx.check_active()
            return raw
        def require_contract(reference, contract):
            entry = reader.entries.get((reference.artifact_id, reference.sha256))
            if entry is None or entry.contract != contract:
                raise AdoptionInvalid('INTERCHANGE_FACTORY_INPUT_CONTRACT')
        images, bindings = [], {}
        for entry in plan.entries:
            binding = entry.binding
            if binding is None:
                continue
            previous = bindings.get(binding.asset_version_id)
            if previous is not None:
                if previous != binding:
                    raise AdoptionInvalid('INTERCHANGE_FACTORY_SOURCE_CONFLICT')
                continue
            require_contract(binding.source, 'WorkingImage')
            image_raw = read(binding.source,1 << 20)
            image = WorkingImage.model_validate_json(image_raw)
            if canonical_bytes(image) != image_raw:
                raise AdoptionInvalid('INTERCHANGE_FACTORY_CANONICAL_MASTER')
            bindings[binding.asset_version_id] = binding.model_copy(deep=True)
            images.append((binding,image))
        entries = []
        for entry in reader.entries.values():
            metadata = reader.evidence(entry.ref.model_copy(deep=True))
            entries.append((entry.ref.model_copy(deep=True),entry.contract,metadata.media_type))
        historical = self.historical
        if historical is not None and callable(getattr(historical, 'bind_inputs', None)):
            command = ctx.lease.command
            historical = historical.bind_inputs(entries=entries, read=read,
                tenant_id=command.tenant_id, project_id=authority.project_id,
                workflow_id=authority.workflow_id, run_id=command.run_id, plan=plan,
                check_active=ctx.check_active, recheck_inputs=reader.recheck)
        def runtime(contract,typed,binding):
            ctx.check_active()
            from photo_worker.v2.interchange_conservative_semantics import qualify_conservative_document
            conservative = qualify_conservative_document(contract, typed, binding, read, require_contract, ctx.check_active,
                None if historical is None else lambda *args: historical.qualify_faces(ctx, authority, *args))
            if conservative is False:
                return False
            result = self.runtime(ctx,authority,contract,typed,binding)
            ctx.check_active()
            return result
        confirmation_epochs = {}
        def confirmation(mask,binding):
            if self.confirmation is None:
                return False
            ctx.check_active()
            result = self.confirmation(ctx,artifacts,authority,mask,binding)
            ctx.check_active()
            if type(result) is not int or result < 1:
                return False
            key = (binding.asset_version_id, mask.artifact.artifact_id,
                   mask.artifact.sha256)
            previous = confirmation_epochs.setdefault(key, result)
            return previous == result
        ancestors = () if historical is None else historical.ancestors(ctx, authority, plan, read, require_contract)
        result = build_prepared_import_recipe_qualifier(canonical_images=images,work_icc_sha=self.icc,
            entries=entries,schemas_jcs=self.schemas,schemas_sha=self.schemas_sha,read_document=read,
            evidence_reader=reader,scratch=artifacts.scratch,max_file_bytes=self.maximum,
            qualify_runtime=runtime,extra_validators=dict(self.extra),check_active=ctx.check_active,
            recheck_inputs=reader.recheck,qualify_occurrence=qualify_occurrence,
            operation_schemas=None if self.operation_schemas is None else dict(self.operation_schemas),
            qualify_confirmation=confirmation if self.confirmation is not None else None,
            background_mappings=background_mappings, prepared_ancestors=ancestors,
            qualify_ancestor=None if historical is None else lambda *args: historical.qualify_ancestor(ctx, authority, *args))
        ctx.check_active()
        return result
