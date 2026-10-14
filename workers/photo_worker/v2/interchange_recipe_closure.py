"""Bounded physical and typed verification of prepared declared recipe inputs."""
from hashlib import sha256
from uuid import UUID

from jsonschema import Draft202012Validator, FormatChecker
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, parse_json
from .adoption import AdoptionInvalid, _schema_refs
from .render_recipe import EditingRecipeV3
from photo_worker.operations.session.contracts import VersionBinding


def prepared_child_contract(contract, schema_name, path):
    """Expected JSON roles from the selected parent schema, never inventory."""
    typed_roles = {
        'DefectCandidates': {('image',): 'WorkingImage', ('source_pixels',): 'WorkingPixels',
                             ('label_map',): 'DefectLabelMap',
                             ('transform_chain', 'steps', '*', 'inverse_field'): 'DenseWarpField'},
        'DefectReview': {('image',): 'WorkingImage', ('proposal',): 'DefectCandidates',
                         ('label_map',): 'DefectLabelMap'},
        'WorkingImage': {('pixels',): 'WorkingPixels', ('alpha',): 'WorkingAlpha',
                         ('transform_chain', 'steps', '*', 'inverse_field'): 'DenseWarpField'},
        'MaskRef': {('artifact',): 'MaskPixels',
                    ('transform_chain', 'steps', '*', 'inverse_field'): 'DenseWarpField'},
        'DescriptorShard': {('frames', '*', 'binding', 'source'): 'WorkingImage',
                            ('frames', '*', 'faces'): 'FaceObservations'},
        'FaceObservations': {('binding', 'source'): 'WorkingImage'},
        'CorrectionPlan': {('items', '*', 'binding', 'source'): 'WorkingImage'},
    }
    resolved_roles = {
        ('params', 'image'): 'WorkingImage', ('params', 'style'): 'StyleV2',
        ('params', 'stats'): 'Stats20', ('absolute_lut', 'artifact'): 'AbsoluteLUTArray',
        ('params', 'override', 'mask_refs', '*', 'mask', 'artifact'): 'MaskPixels',
        ('params', 'override', 'mask_refs', '*', 'mask', 'transform_chain', 'steps', '*', 'inverse_field'): 'DenseWarpField',
    }
    typed_roles['ResolvedStyle'] = resolved_roles
    typed_roles['Stats20'] = {('source_image',): 'WorkingImage'}
    typed_roles['Override'] = {
        ('mask_refs', '*', 'mask', 'artifact'): 'MaskPixels',
        ('mask_refs', '*', 'mask', 'transform_chain', 'steps', '*', 'inverse_field'): 'DenseWarpField',
        ('manual_landmarks', 'transform_chain', 'steps', '*', 'inverse_field'): 'DenseWarpField',
    }
    if contract in typed_roles:
        expected = typed_roles[contract].get(path)
        if expected is None:
            raise AdoptionInvalid('INTERCHANGE_CLOSURE_TYPED_CHILD')
        return expected
    if contract == 'Params' and schema_name in ('PortraitHairParams', 'PortraitClothingParams'):
        roles = {('base', 'source'): 'WorkingImage', ('mask',): 'MaskRef', ('protected',): 'MaskRef'}
        expected = roles.get(path)
        if expected is None:
            raise AdoptionInvalid('INTERCHANGE_CLOSURE_PARAMS_CHILD')
        return expected
    if contract == 'Params' and schema_name in ('DenoiseParams', 'UpscaleParams', 'StatsParams', 'DustRepairParams', 'PortraitParams', 'DefectRepairParams'):
        roles = {('image',): 'WorkingImage'}
        if schema_name == 'StatsParams':
            roles.update({('person', 'artifact'): 'MaskPixels',
                          ('person', 'transform_chain', 'steps', '*', 'inverse_field'): 'DenseWarpField'})
        elif schema_name == 'DustRepairParams':
            roles.update({('proposal',): 'DustCandidates', ('review', 'proposal'): 'DustCandidates',
                          ('review', 'base'): 'WorkingImage'})
        elif schema_name in ('PortraitParams', 'DefectRepairParams'):
            roles[('resolved',)] = 'ResolvedStyle'
            if schema_name == 'DefectRepairParams':
                roles.update({('proposal',): 'DefectCandidates', ('review',): 'DefectReview'})
                mask_paths = [('protected',)]
            else:
                mask_paths = [('regions', '*', 'mask'), ('protected', '*')]
            for prefix in mask_paths:
                roles[prefix + ('artifact',)] = 'MaskPixels'
                roles[prefix + ('transform_chain', 'steps', '*', 'inverse_field')] = 'DenseWarpField'
        expected = roles.get(path)
        if expected is None:
            raise AdoptionInvalid('INTERCHANGE_CLOSURE_PARAMS_CHILD')
        return expected
    if contract == 'LayerStack':
        if path == ('source', 'source'):
            return 'WorkingImage'
        if path == ('layers', '*', 'params'):
            return 'Params'
        if len(path) >= 4 and path[:3] == ('layers', '*', 'mask') and path[-1] == 'mask':
            return 'MaskRef'
    return None


class PreparedRecipeClosureQualifier:
    """Construct from scheduler-authenticated input entries and pinned schemas.

    Semantic ports must qualify installed operators/source geometry and physical
    binary arrays. Structural JSON validity never substitutes for these checks.
    No output refs, live catalogue scan or archive contract labels are accepted.
    """
    def __init__(self, *, entries, schemas_jcs, schemas_sha, read_document,
                 qualify_document, qualify_binary, check_active=lambda: None, recheck_inputs=None,
                 qualify_occurrence=None, operation_schemas=None):
        if (not isinstance(schemas_jcs, bytes) or not 2 <= len(schemas_jcs) <= 16 << 20
                or sha256(schemas_jcs).hexdigest() != schemas_sha
                or not all(callable(port) for port in (read_document, qualify_document, qualify_binary, check_active))):
            raise ValueError('INTERCHANGE_CLOSURE_INSTALLATION')
        self.schemas = parse_json(schemas_jcs, max_bytes=16 << 20)
        if not isinstance(self.schemas.get('$defs'), dict):
            raise ValueError('INTERCHANGE_CLOSURE_SCHEMAS')
        from .interchange_operator_schemas import build_interchange_operator_schemas
        self.operation_schemas = build_interchange_operator_schemas(self.schemas['$defs'], operation_schemas)
        self.entries = {}
        for reference, contract, media_type in entries:
            media_ok = (media_type in ('image/jpeg', 'image/png', 'image/tiff')
                        if contract == 'StyleBackgroundAsset' else
                        media_type in ('application/json', 'application/octet-stream', 'application/x-npy'))
            if (not isinstance(reference, ArtifactRef) or reference.artifact_id.int == 0
                    or not isinstance(contract, str) or not 1 <= len(contract) <= 120
                    or not media_ok
                    or reference.artifact_id in self.entries):
                raise ValueError('INTERCHANGE_CLOSURE_ENTRIES')
            self.entries[reference.artifact_id] = (reference.model_copy(deep=True), contract, media_type)
            if len(self.entries) > 30000:
                raise ValueError('INTERCHANGE_CLOSURE_LIMIT')
        self.read, self.qualify_document, self.qualify_binary = read_document, qualify_document, qualify_binary
        if recheck_inputs is not None and not callable(recheck_inputs):
            raise ValueError('INTERCHANGE_CLOSURE_INSTALLATION')
        if qualify_occurrence is not None and not callable(qualify_occurrence):
            raise ValueError('INTERCHANGE_CLOSURE_INSTALLATION')
        self.qualify_occurrence = qualify_occurrence
        self.recheck_inputs = recheck_inputs
        self.active = check_active
        self._qualified_closures = {}

    def __call__(self, reference, recipe, binding):
        # Own all caller identities before callbacks or object IO. In particular,
        # a caller mutation must not change the expected list midway through DFS.
        reference = ArtifactRef.model_validate_json(canonical_bytes(reference))
        binding = VersionBinding.model_validate_json(canonical_bytes(binding))
        key = (reference.artifact_id, reference.sha256, canonical_bytes(binding))
        self._qualified_closures.pop(key, None)
        recipe = EditingRecipeV3.model_validate_json(canonical_bytes(recipe))
        self.active()
        if recipe.source != binding or recipe.learning_version_id is not None:
            raise AdoptionInvalid('INTERCHANGE_CLOSURE_RECIPE_BINDING')
        recipe_body = canonical_bytes(recipe)
        if sha256(recipe_body).hexdigest() != reference.sha256:
            raise AdoptionInvalid('INTERCHANGE_CLOSURE_RECIPE_HASH')
        active, visited = set(), set()
        total = 0
        params_schemas = {}

        def visit(ref, depth, expected=None, parent=None, parent_contract=None, path=(), parent_body=None):
            nonlocal total
            self.active()
            if depth > 32:
                raise AdoptionInvalid('INTERCHANGE_CLOSURE_LIMIT')
            evidence = self.entries.get(ref.artifact_id)
            if evidence is None or evidence[0] != ref or expected is not None and evidence[1] != expected:
                raise AdoptionInvalid('INTERCHANGE_CLOSURE_UNDECLARED_OR_TYPE')
            if ref.artifact_id in active:
                raise AdoptionInvalid('INTERCHANGE_CLOSURE_CYCLE')
            # Occurrence authority runs before byte deduplication. One physical
            # document may be referenced from distinct descriptor frame contexts.
            if evidence[1] in ('DescriptorShard', 'FaceObservations') and self.qualify_occurrence is None:
                raise AdoptionInvalid('INTERCHANGE_CLOSURE_OCCURRENCE_REQUIRED')
            if self.qualify_occurrence is not None:
                from copy import deepcopy
                if self.qualify_occurrence(dict(
                        reference=ref.model_copy(deep=True), contract=evidence[1],
                        parent=None if parent is None else parent.model_copy(deep=True),
                        parent_contract=parent_contract, path=tuple(path),
                        parent_body=deepcopy(parent_body), binding=binding.model_copy(deep=True))) is not True:
                    raise AdoptionInvalid('INTERCHANGE_CLOSURE_OCCURRENCE')
                self.active()
            if ref.artifact_id in visited:
                return
            if len(visited) + len(active) >= 30000:
                raise AdoptionInvalid('INTERCHANGE_CLOSURE_LIMIT')
            contract, media_type = evidence[1:]
            if media_type != 'application/json':
                if self.qualify_binary(ref.model_copy(deep=True), contract, binding.model_copy(deep=True)) is not True:
                    raise AdoptionInvalid('INTERCHANGE_CLOSURE_BINARY')
            else:
                schema_name = params_schemas[ref.artifact_id][0] if contract == 'Params' and ref.artifact_id in params_schemas else contract
                if contract == 'Params' and ref.artifact_id not in params_schemas:
                    raise AdoptionInvalid('INTERCHANGE_CLOSURE_PARAMS_OPERATION')
                if schema_name not in self.schemas['$defs']:
                    raise AdoptionInvalid('INTERCHANGE_CLOSURE_UNKNOWN_CONTRACT')
                raw = self.read(ref.model_copy(deep=True), min(8 << 20, (128 << 20) - total))
                self.active()
                if not isinstance(raw, bytes) or not 2 <= len(raw) <= min(8 << 20, (128 << 20) - total) or sha256(raw).hexdigest() != ref.sha256:
                    raise AdoptionInvalid('INTERCHANGE_CLOSURE_DOCUMENT_HASH')
                total += len(raw)
                value = parse_json(raw, max_bytes=8 << 20)
                if canonical_bytes(value) != raw:
                    raise AdoptionInvalid('INTERCHANGE_CLOSURE_CANONICAL')
                validator = Draft202012Validator({**self.schemas, '$ref': '#/$defs/' + schema_name}, format_checker=FormatChecker())
                if not validator.is_valid(value):
                    raise AdoptionInvalid('INTERCHANGE_CLOSURE_SCHEMA')
                if ref == reference and raw != recipe_body:
                    raise AdoptionInvalid('INTERCHANGE_CLOSURE_RECIPE_CHANGED')
                params_qualifier = getattr(self.qualify_document, 'qualify_params', None)
                if contract == 'Params' and callable(params_qualifier):
                    qualified = params_qualifier(schema_name, parse_json(raw, max_bytes=8 << 20), binding.model_copy(deep=True))
                else:
                    qualified = self.qualify_document(contract, parse_json(raw, max_bytes=8 << 20), binding.model_copy(deep=True))
                if qualified is not True:
                    raise AdoptionInvalid('INTERCHANGE_CLOSURE_SEMANTICS')
                if contract == 'LayerStack':
                    for layer in value.get('layers', []):
                        operation = (layer['operation'], layer['operation_version'])
                        selected = self.operation_schemas.get(operation)
                        if selected is None:
                            raise AdoptionInvalid('INTERCHANGE_CLOSURE_LAYER_OPERATION')
                        identifier = ArtifactRef.model_validate_json(canonical_bytes(layer['params'])).artifact_id
                        if identifier in params_schemas and params_schemas[identifier] != (selected, *operation):
                            raise AdoptionInvalid('INTERCHANGE_CLOSURE_PARAMS_CONFLICT')
                        params_schemas[identifier] = (selected, *operation)
                active.add(ref.artifact_id)
                try:
                    # Style and Override background IDs are portable typed dependencies,
                    # not ArtifactRef objects discoverable by schema traversal.
                    if contract in ('StyleV2', 'Override'):
                        extensions = (value.get('extensions') if contract == 'StyleV2'
                                      else value.get('modules', {}).get('extensions'))
                        background = extensions.get('background') if isinstance(extensions, dict) else None
                        if isinstance(background, dict) and background.get('mode') == 'asset':
                            portable = background.get('asset_id')
                            try:
                                identifier = UUID(portable)
                                if identifier.int == 0 or str(identifier) != portable:
                                    raise ValueError('noncanonical portable ID')
                            except (ValueError, TypeError, AttributeError) as error:
                                raise AdoptionInvalid('INTERCHANGE_CLOSURE_BACKGROUND_ID') from error
                            background_entry = self.entries.get(identifier)
                            if background_entry is None or background_entry[1] != 'StyleBackgroundAsset':
                                raise AdoptionInvalid('INTERCHANGE_CLOSURE_BACKGROUND_INPUT')
                            path = (('extensions', 'background', 'asset_id') if contract == 'StyleV2'
                                    else ('modules', 'extensions', 'background', 'asset_id'))
                            visit(background_entry[0].model_copy(deep=True), depth + 1,
                                  'StyleBackgroundAsset', ref, contract, path, value)
                    for path, child in _schema_refs(self.schemas['$defs'][schema_name], value, self.schemas['$defs'], validator):
                        expected_child = prepared_child_contract(contract, schema_name, path)
                        visit(ArtifactRef(artifact_id=child.artifact_id, sha256=child.sha256), depth + 1, expected_child,
                              ref, contract, path, value)
                finally:
                    active.remove(ref.artifact_id)
            self.active()
            visited.add(ref.artifact_id)

        # Root and immediate recipe contracts cannot be relabelled by inventory.
        expected = [(reference, 'EditingRecipeV3'), (recipe.source.source, 'WorkingImage'),
                    (recipe.base_style, 'StyleV2'), *[(ref, 'Override') for ref in recipe.overrides]]
        expected += [(ref, contract) for ref, contract in ((recipe.session_corrections, 'CorrectionPlan'),
                     (recipe.layer_stack, 'LayerStack'), (recipe.protected_mask, 'MaskRef')) if ref is not None]
        for ref, contract in expected:
            evidence = self.entries.get(ref.artifact_id)
            if evidence is None or evidence[0] != ref or evidence[1] != contract or evidence[2] != 'application/json':
                raise AdoptionInvalid('INTERCHANGE_CLOSURE_UNDECLARED_OR_TYPE')
        for ref, contract in expected:
            visit(ref, 0, contract)
        self.active()
        if self.recheck_inputs is not None:
            self.recheck_inputs()
        self.active()
        # Retain immutable pins only after every semantic/physical check passed.
        # Exclude the recipe root, already retained by v2_documents.artifact_id.
        pins = tuple(sorted(((identifier, self.entries[identifier][0].sha256)
                             for identifier in visited if identifier != reference.artifact_id),
                            key=lambda item: str(item[0])))
        self._qualified_closures[key] = pins
        return True

    def qualified_document_refs(self, reference, binding):
        """Fresh SQL-ready GC refs for an exact successfully qualified binding.

        No caller may supply a substitute list or infer closure from all task
        inputs. A failed requalification invalidates the prior receipt.
        """
        reference = ArtifactRef.model_validate_json(canonical_bytes(reference))
        binding = VersionBinding.model_validate_json(canonical_bytes(binding))
        key = (reference.artifact_id, reference.sha256, canonical_bytes(binding))
        try:
            self.active()
            if self.recheck_inputs is not None:
                self.recheck_inputs()
            self.active()
        except BaseException:
            self._qualified_closures.pop(key, None)
            raise
        pins = self._qualified_closures.get(key)
        if pins is None:
            raise AdoptionInvalid('INTERCHANGE_CLOSURE_RECEIPT_REQUIRED')
        return tuple(dict(artifact_id=str(identifier), sha256=digest,
            purpose=sha256(('interchange/recipe/'+str(reference.artifact_id)+'/dependency/'+str(identifier)).encode()).hexdigest(),
            ordinal=ordinal) for ordinal, (identifier, digest) in enumerate(pins))
