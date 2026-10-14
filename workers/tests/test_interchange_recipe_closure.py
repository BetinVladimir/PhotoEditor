from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_recipe_closure import PreparedRecipeClosureQualifier
from test_interchange_recipe import fixture


def closure_fixture():
    recipe, _, _, _, _ = fixture()
    rows = [(recipe.source.source, 'WorkingImage'), (recipe.base_style, 'StyleV2'),
            (recipe.session_corrections, 'CorrectionPlan'), (recipe.overrides[0], 'Override'),
            (recipe.layer_stack, 'LayerStack'), (recipe.protected_mask, 'MaskRef')]
    bodies = {}
    for ref, _ in rows:
        ref.sha256 = sha256(b'{}').hexdigest()
        bodies[ref.artifact_id] = b'{}'
    root_body = canonical_bytes(recipe)
    root = ArtifactRef(artifact_id=uuid4(), sha256=sha256(root_body).hexdigest())
    bodies[root.artifact_id] = root_body
    schema_ref = {'$ref': '#/$defs/ArtifactRef'}
    defs = {name: {'type': 'object'} for _, name in rows}
    defs['ArtifactRef'] = {'type': 'object', 'required': ['artifact_id', 'sha256'],
        'properties': {'artifact_id': {'type': 'string'}, 'sha256': {'type': 'string'}}, 'additionalProperties': False}
    defs['EditingRecipeV3'] = {'type': 'object', 'properties': {
        'source': {'type': 'object', 'properties': {'source': schema_ref}},
        'base_style': schema_ref, 'session_corrections': schema_ref,
        'overrides': {'type': 'array', 'items': schema_ref}, 'layer_stack': schema_ref,
        'protected_mask': schema_ref}}
    schemas = canonical_bytes({'$defs': defs})
    args = dict(entries=[(root, 'EditingRecipeV3', 'application/json'), *[(ref, name, 'application/json') for ref, name in rows]],
        schemas_jcs=schemas, schemas_sha=sha256(schemas).hexdigest(),
        read_document=lambda ref, limit: bodies[ref.artifact_id],
        qualify_document=lambda *args: True, qualify_binary=lambda *args: True)
    return recipe, root, args


def test_prepared_recipe_closure_walks_every_declared_dependency():
    recipe, root, args = closure_fixture();reads=[];read=args['read_document']
    args['read_document'] = lambda ref, limit: (reads.append(ref.artifact_id), read(ref, limit))[1]
    assert PreparedRecipeClosureQualifier(**args)(root, recipe, recipe.source) is True
    assert set(reads) == {row[0].artifact_id for row in args['entries']}
    assert len(reads) == len(set(reads))


@pytest.mark.parametrize('change', ['missing', 'type', 'hash', 'semantics', 'truthy'])
def test_prepared_recipe_closure_rejects_missing_or_unqualified_dependencies(change):
    recipe, root, args = closure_fixture()
    if change == 'missing':
        args['entries'] = args['entries'][:-1]
    elif change == 'type':
        ref, _, media = args['entries'][-1];args['entries'][-1] = (ref, 'StyleV2', media)
    elif change == 'hash':
        args['read_document'] = lambda *args: b'{"substitution":true}'
    else:
        args['qualify_document'] = lambda *args: 1 if change == 'truthy' else False
    with pytest.raises(AdoptionInvalid):
        PreparedRecipeClosureQualifier(**args)(root, recipe, recipe.source)


def test_prepared_recipe_closure_requires_pinned_schemas_and_unique_inventory():
    _, _, args = closure_fixture();args['schemas_sha'] = '0'*64
    with pytest.raises(ValueError, match='INSTALLATION'):
        PreparedRecipeClosureQualifier(**args)
    _, _, args = closure_fixture();args['entries'].append(args['entries'][0])
    with pytest.raises(ValueError, match='ENTRIES'):
        PreparedRecipeClosureQualifier(**args)


def test_gc_receipt_requires_success_and_owns_exact_dependency_pins():
    recipe, root, args = closure_fixture()
    qualifier = PreparedRecipeClosureQualifier(**args)
    with pytest.raises(AdoptionInvalid, match='RECEIPT_REQUIRED'):
        qualifier.qualified_document_refs(root, recipe.source)
    qualifier(root, recipe, recipe.source)
    refs = qualifier.qualified_document_refs(root, recipe.source)
    expected = {str(row[0].artifact_id) for row in args['entries'] if row[0] != root}
    assert {ref['artifact_id'] for ref in refs} == expected
    assert [ref['ordinal'] for ref in refs] == list(range(len(refs)))
    refs[0]['sha256'] = '0'*64
    assert qualifier.qualified_document_refs(root, recipe.source)[0]['sha256'] != '0'*64
    other = recipe.source.model_copy(deep=True)
    other.asset_version_id = uuid4()
    with pytest.raises(AdoptionInvalid, match='RECEIPT_REQUIRED'):
        qualifier.qualified_document_refs(root, other)


def test_failed_requalification_invalidates_gc_receipt():
    recipe, root, args = closure_fixture()
    qualifier = PreparedRecipeClosureQualifier(**args)
    qualifier(root, recipe, recipe.source)
    qualifier.qualify_document = lambda *unused: False
    with pytest.raises(AdoptionInvalid):
        qualifier(root, recipe, recipe.source)
    with pytest.raises(AdoptionInvalid, match='RECEIPT_REQUIRED'):
        qualifier.qualified_document_refs(root, recipe.source)


def test_closure_owns_root_recipe_and_binding_before_document_callbacks():
    recipe, root, args = closure_fixture()
    original_root = root.model_copy(deep=True)
    original_binding = recipe.source.model_copy(deep=True)
    expected = {str(row[0].artifact_id) for row in args['entries'] if row[0] != root}
    read = args['read_document']
    changed = False
    def mutating_read(reference, limit):
        nonlocal changed
        body = read(reference, limit)
        if not changed:
            changed = True
            root.artifact_id = uuid4()
            root.sha256 = '0' * 64
            recipe.source.asset_version_id = uuid4()
            recipe.base_style.artifact_id = uuid4()
            recipe.overrides.clear()
        return body
    args['read_document'] = mutating_read
    qualifier = PreparedRecipeClosureQualifier(**args)
    assert qualifier(root, recipe, recipe.source) is True
    receipt = qualifier.qualified_document_refs(original_root, original_binding)
    assert {item['artifact_id'] for item in receipt} == expected
    with pytest.raises(AdoptionInvalid, match='RECEIPT_REQUIRED'):
        qualifier.qualified_document_refs(root, recipe.source)


def test_receipt_lookup_owns_identity_before_active_callback():
    recipe, root, args = closure_fixture()
    qualifier = PreparedRecipeClosureQualifier(**args)
    qualifier(root, recipe, recipe.source)
    expected = qualifier.qualified_document_refs(root, recipe.source)
    binding = recipe.source.model_copy(deep=True)
    def mutate_caller():
        root.artifact_id = uuid4()
        binding.asset_version_id = uuid4()
    qualifier.active = mutate_caller
    assert qualifier.qualified_document_refs(root, binding) == expected


def test_invalid_recipe_requalification_revokes_prior_gc_receipt():
    recipe, root, args = closure_fixture()
    qualifier = PreparedRecipeClosureQualifier(**args)
    qualifier(root, recipe, recipe.source)
    recipe.renderer_version = 'invalid-renderer'
    with pytest.raises(ValueError):
        qualifier(root, recipe, recipe.source)
    with pytest.raises(AdoptionInvalid, match='RECEIPT_REQUIRED'):
        qualifier.qualified_document_refs(root, recipe.source)


@pytest.mark.parametrize('mode',['unknown','conflict'])
def test_layer_params_cannot_use_unknown_or_conflicting_operation_schema(mode):
    recipe,root,args=closure_fixture()
    params=ArtifactRef(artifact_id=uuid4(),sha256=sha256(b'{}').hexdigest())
    layers=[{'operation':'portrait.hair' if mode=='conflict' else 'unknown.operation',
             'operation_version':'1','params':params.model_dump(mode='json')}]
    if mode=='conflict':layers.append({'operation':'portrait.clothing','operation_version':'1','params':params.model_dump(mode='json')})
    layer_raw=canonical_bytes({'layers':layers})
    recipe.layer_stack.sha256=sha256(layer_raw).hexdigest()
    root_raw=canonical_bytes(recipe);root.sha256=sha256(root_raw).hexdigest()
    read=args['read_document']
    def replacement(ref,maximum):
        if ref.artifact_id==root.artifact_id:return root_raw
        if ref.artifact_id==recipe.layer_stack.artifact_id:return layer_raw
        return read(ref,maximum)
    args['read_document']=replacement
    code='LAYER_OPERATION' if mode=='unknown' else 'PARAMS_CONFLICT'
    with pytest.raises(AdoptionInvalid,match=code):
        PreparedRecipeClosureQualifier(**args)(root,recipe,recipe.source)


@pytest.mark.parametrize('schema',['PortraitHairParams','PortraitClothingParams'])
def test_local_params_child_contracts_are_derived_from_schema(schema):
    from photo_worker.v2.interchange_recipe_closure import prepared_child_contract
    assert prepared_child_contract('Params',schema,('base','source'))=='WorkingImage'
    assert prepared_child_contract('Params',schema,('mask',))=='MaskRef'
    assert prepared_child_contract('Params',schema,('protected',))=='MaskRef'
    with pytest.raises(AdoptionInvalid,match='PARAMS_CHILD'):
        prepared_child_contract('Params',schema,('unrecognized_ref',))


def test_layer_nested_mask_and_params_references_have_expected_contracts():
    from photo_worker.v2.interchange_recipe_closure import prepared_child_contract
    assert prepared_child_contract('LayerStack','LayerStack',('layers','*','params'))=='Params'
    assert prepared_child_contract('LayerStack','LayerStack',('source','source'))=='WorkingImage'
    for path in [('layers','*','mask','mask'),('layers','*','mask','left','operand','mask')]:
        assert prepared_child_contract('LayerStack','LayerStack',path)=='MaskRef'


def test_final_input_recheck_failure_prevents_gc_receipt_and_revokes_saved_receipt():
    recipe,root,args=closure_fixture()
    calls=[]
    def current():calls.append(True)
    args['recheck_inputs']=current
    qualifier=PreparedRecipeClosureQualifier(**args)
    assert qualifier(root,recipe,recipe.source) is True
    assert len(calls)==1
    assert qualifier.qualified_document_refs(root,recipe.source)
    assert len(calls)==2
    def changed():raise AdoptionInvalid('IMMUTABLE_INPUT_CHANGED')
    qualifier.recheck_inputs=changed
    with pytest.raises(AdoptionInvalid,match='IMMUTABLE_INPUT_CHANGED'):
        qualifier.qualified_document_refs(root,recipe.source)
    qualifier.recheck_inputs=current
    with pytest.raises(AdoptionInvalid,match='RECEIPT_REQUIRED'):
        qualifier.qualified_document_refs(root,recipe.source)
    qualifier.recheck_inputs=changed
    with pytest.raises(AdoptionInvalid,match='IMMUTABLE_INPUT_CHANGED'):
        qualifier(root,recipe,recipe.source)
    qualifier.recheck_inputs=current
    with pytest.raises(AdoptionInvalid,match='RECEIPT_REQUIRED'):
        qualifier.qualified_document_refs(root,recipe.source)


def test_cancelled_receipt_lookup_revokes_prior_qualification():
    recipe,root,args=closure_fixture()
    qualifier=PreparedRecipeClosureQualifier(**args)
    qualifier(root,recipe,recipe.source)
    def cancelled():raise RuntimeError('cancelled')
    qualifier.active=cancelled
    with pytest.raises(RuntimeError,match='cancelled'):
        qualifier.qualified_document_refs(root,recipe.source)
    qualifier.active=lambda:None
    with pytest.raises(AdoptionInvalid,match='RECEIPT_REQUIRED'):
        qualifier.qualified_document_refs(root,recipe.source)


@pytest.mark.parametrize('contract,path,expected', [
    ('WorkingImage', ('pixels',), 'WorkingPixels'),
    ('WorkingImage', ('alpha',), 'WorkingAlpha'),
    ('WorkingImage', ('transform_chain', 'steps', '*', 'inverse_field'), 'DenseWarpField'),
    ('MaskRef', ('artifact',), 'MaskPixels'),
    ('MaskRef', ('transform_chain', 'steps', '*', 'inverse_field'), 'DenseWarpField'),
    ('DescriptorShard', ('frames', '*', 'binding', 'source'), 'WorkingImage'),
    ('DescriptorShard', ('frames', '*', 'faces'), 'FaceObservations'),
    ('FaceObservations', ('binding', 'source'), 'WorkingImage'),
    ('CorrectionPlan', ('items', '*', 'binding', 'source'), 'WorkingImage'),
    ('ResolvedStyle', ('params', 'image'), 'WorkingImage'),
    ('ResolvedStyle', ('params', 'style'), 'StyleV2'),
    ('ResolvedStyle', ('params', 'stats'), 'Stats20'),
    ('ResolvedStyle', ('absolute_lut', 'artifact'), 'AbsoluteLUTArray'),
    ('ResolvedStyle', ('params', 'override', 'mask_refs', '*', 'mask', 'artifact'), 'MaskPixels'),
    ('ResolvedStyle', ('params', 'override', 'mask_refs', '*', 'mask', 'transform_chain', 'steps', '*', 'inverse_field'), 'DenseWarpField'),
    ('Stats20', ('source_image',), 'WorkingImage'),
])
def test_prepared_child_contract_retains_frame_and_binary_roles(contract, path, expected):
    from photo_worker.v2.interchange_recipe_closure import prepared_child_contract
    assert prepared_child_contract(contract, contract, path) == expected
    with pytest.raises(AdoptionInvalid, match='TYPED_CHILD'):
        prepared_child_contract(contract, contract, ('invented_reference',))


def test_prepared_working_pixels_cannot_be_relabelled_in_inventory():
    recipe, root, args = closure_fixture()
    pixels_body = b'physical-array'
    pixels = ArtifactRef(artifact_id=uuid4(), sha256=sha256(pixels_body).hexdigest())
    working_body = canonical_bytes({'pixels': pixels})
    recipe.source.source.sha256 = sha256(working_body).hexdigest()
    root_body = canonical_bytes(recipe)
    root.sha256 = sha256(root_body).hexdigest()
    from photo_worker.contracts.base import parse_json
    schemas = parse_json(args['schemas_jcs'])
    schemas['$defs']['WorkingImage'] = {'type': 'object', 'properties': {
        'pixels': {'$ref': '#/$defs/ArtifactRef'}}}
    args['schemas_jcs'] = canonical_bytes(schemas)
    args['schemas_sha'] = sha256(args['schemas_jcs']).hexdigest()
    args['entries'] = [(ref, contract, media) for ref, contract, media in args['entries']]
    for i, (ref, contract, media) in enumerate(args['entries']):
        if contract == 'WorkingImage':
            args['entries'][i] = (recipe.source.source.model_copy(deep=True), contract, media)
        elif contract == 'EditingRecipeV3':
            args['entries'][i] = (root.model_copy(deep=True), contract, media)
    args['entries'].append((pixels, 'MaskPixels', 'application/x-npy'))
    old_read = args['read_document']
    def read(ref, limit):
        if ref.artifact_id == root.artifact_id:
            return root_body
        if ref.artifact_id == recipe.source.source.artifact_id:
            return working_body
        return old_read(ref, limit)
    args['read_document'] = read
    with pytest.raises(AdoptionInvalid, match='UNDECLARED_OR_TYPE'):
        PreparedRecipeClosureQualifier(**args)(root, recipe, recipe.source)



def test_prepared_closure_qualifies_repeated_edges_before_byte_deduplication():
    recipe, root, args = closure_fixture()
    calls = []
    def occurrence(context):
        calls.append(context)
        # Mutating callback-owned data must not change traversal identities.
        context['binding'].canvas_hash = 'f' * 64
        if context['parent_body'] is not None:
            context['parent_body'].clear()
        return True
    args['qualify_occurrence'] = occurrence
    assert PreparedRecipeClosureQualifier(**args)(root, recipe, recipe.source) is True
    working = [item for item in calls if item['reference'].artifact_id == recipe.source.source.artifact_id]
    assert len(working) >= 2
    assert any(item['parent'] == root for item in working)
    assert any(item['parent'] is None for item in working)


def test_prepared_closure_repeated_occurrence_refusal_is_terminal():
    recipe, root, args = closure_fixture()
    counts = {}
    def occurrence(context):
        identifier = context['reference'].artifact_id
        counts[identifier] = counts.get(identifier, 0) + 1
        return counts[identifier] == 1
    args['qualify_occurrence'] = occurrence
    with pytest.raises(AdoptionInvalid, match='OCCURRENCE'):
        PreparedRecipeClosureQualifier(**args)(root, recipe, recipe.source)


def test_prepared_closure_requires_callable_occurrence_installation():
    _, _, args = closure_fixture()
    args['qualify_occurrence'] = True
    with pytest.raises(ValueError, match='INSTALLATION'):
        PreparedRecipeClosureQualifier(**args)


@pytest.mark.parametrize('mode', ['valid', 'uninstalled', 'conflict', 'denied'])
def test_installed_operator_schema_qualifies_exact_params_without_dropping_layers(mode):
    from photo_worker.contracts.base import parse_json
    recipe, root, args = closure_fixture()
    params = ArtifactRef(artifact_id=uuid4(), sha256=sha256(b'{}').hexdigest())
    layers = [{'operation':'uninstalled.denoise' if mode == 'uninstalled' else 'image.denoise','operation_version':'1','params':params.model_dump(mode='json')}]
    if mode == 'conflict':
        layers.append({'operation':'other.denoise','operation_version':'1','params':params.model_dump(mode='json')})
    layer_raw = canonical_bytes({'layers':layers})
    recipe.layer_stack.sha256 = sha256(layer_raw).hexdigest()
    root_raw = canonical_bytes(recipe); root.sha256 = sha256(root_raw).hexdigest()
    schemas = parse_json(args['schemas_jcs'])
    schemas['$defs']['DenoiseParams'] = {'type':'object','additionalProperties':False}
    schemas['$defs']['LayerStack'] = {'type':'object','properties':{'layers':{'type':'array','items':{
        'type':'object','properties':{'params':{'$ref':'#/$defs/ArtifactRef'}}}}}}
    args['schemas_jcs'] = canonical_bytes(schemas)
    args['schemas_sha'] = sha256(args['schemas_jcs']).hexdigest()
    args['entries'].append((params,'Params','application/json'))
    original = args['read_document']
    def read(reference, maximum):
        if reference == root: return root_raw
        if reference == recipe.layer_stack: return layer_raw
        if reference == params: return b'{}'
        return original(reference, maximum)
    args['read_document'] = read
    operations = {('image.denoise','1'):'DenoiseParams',('other.denoise','1'):'DenoiseParams'}
    args['operation_schemas'] = operations if mode != 'uninstalled' else {}
    seen = []
    def qualify(contract, value, binding):
        seen.append(contract)
        return not (mode == 'denied' and contract == 'Params')
    args['qualify_document'] = qualify
    policy = PreparedRecipeClosureQualifier(**args)
    operations.clear()
    if mode == 'valid':
        assert policy(root, recipe, recipe.source) is True
        assert seen.count('Params') == 1
    else:
        with pytest.raises(AdoptionInvalid): policy(root, recipe, recipe.source)


@pytest.mark.parametrize('registry', [[], {('unknown','1'):'Missing'},
    {('portrait.hair','1'):'StyleV2'}, {('bad',):'StyleV2'}, {('a'*81,'1'):'StyleV2'}])
def test_operator_schema_installation_rejects_invalid_or_replaced_builtin(registry):
    _, _, args = closure_fixture()
    with pytest.raises(ValueError, match='OPERATORS'):
        PreparedRecipeClosureQualifier(**args, operation_schemas=registry)


@pytest.mark.parametrize('schema,path,expected', [
    ('DenoiseParams',('image',),'WorkingImage'),
    ('DustRepairParams',('proposal',),'DustCandidates'),
    ('DustRepairParams',('review','proposal'),'DustCandidates'),
    ('DustRepairParams',('review','base'),'WorkingImage'),
    ('PortraitParams',('resolved',),'ResolvedStyle'),
    ('PortraitParams',('regions','*','mask','artifact'),'MaskPixels'),
    ('PortraitParams',('protected','*','artifact'),'MaskPixels'),
    ('PortraitParams',('protected','*','transform_chain','steps','*','inverse_field'),'DenseWarpField'),
    ('DefectRepairParams',('proposal',),'DefectCandidates'),
    ('DefectRepairParams',('review',),'DefectReview'),
    ('DefectRepairParams',('protected','artifact'),'MaskPixels'),
    ('DefectRepairParams',('protected','transform_chain','steps','*','inverse_field'),'DenseWarpField'),
])
def test_concrete_operation_params_children_keep_production_contracts(schema,path,expected):
    from photo_worker.v2.interchange_recipe_closure import prepared_child_contract
    assert prepared_child_contract('Params',schema,path)==expected


@pytest.mark.parametrize('schema,path', [
    ('DenoiseParams',('mask',)), ('DustRepairParams',('review','approved',)),
    ('PortraitParams',('regions','*','mask')), ('DefectRepairParams',('protected',)),
    ('DefectRepairParams',('proposal','artifact')),
])
def test_concrete_operation_params_unknown_artifact_paths_are_rejected(schema,path):
    from photo_worker.v2.interchange_recipe_closure import prepared_child_contract
    with pytest.raises(AdoptionInvalid,match='PARAMS_CHILD'):
        prepared_child_contract('Params',schema,path)


@pytest.mark.parametrize('proposal_contract', ['DustCandidates','MaskRef'])
def test_dust_proposal_role_is_checked_during_actual_closure_traversal(proposal_contract):
    from photo_worker.contracts.base import parse_json
    recipe,root,args=closure_fixture()
    proposal=ArtifactRef(artifact_id=uuid4(),sha256=sha256(b'{}').hexdigest())
    params_raw=canonical_bytes({'image':recipe.source.source,'proposal':proposal,'review':None})
    params=ArtifactRef(artifact_id=uuid4(),sha256=sha256(params_raw).hexdigest())
    layer_raw=canonical_bytes({'layers':[{'operation':'sensor.dust_repair','operation_version':'1','params':params}]})
    recipe.layer_stack.sha256=sha256(layer_raw).hexdigest()
    root_raw=canonical_bytes(recipe);root.sha256=sha256(root_raw).hexdigest()
    schemas=parse_json(args['schemas_jcs']);ref={'$ref':'#/$defs/ArtifactRef'}
    schemas['$defs']['LayerStack']={'type':'object','properties':{'layers':{'type':'array','items':{'type':'object','properties':{'params':ref}}}}}
    schemas['$defs']['DustRepairParams']={'type':'object','properties':{'image':ref,'proposal':ref,'review':{'type':'null'}}}
    schemas['$defs']['DustCandidates']={'type':'object'}
    args['schemas_jcs']=canonical_bytes(schemas);args['schemas_sha']=sha256(args['schemas_jcs']).hexdigest()
    args['operation_schemas']={('sensor.dust_repair','1'):'DustRepairParams'}
    args['entries'] += [(params,'Params','application/json'),(proposal,proposal_contract,'application/json')]
    old=args['read_document']
    def read(reference,maximum):
        if reference==root:return root_raw
        if reference==recipe.layer_stack:return layer_raw
        if reference==params:return params_raw
        if reference==proposal:return b'{}'
        return old(reference,maximum)
    args['read_document']=read
    policy=PreparedRecipeClosureQualifier(**args)
    if proposal_contract=='DustCandidates':assert policy(root,recipe,recipe.source) is True
    else:
        with pytest.raises(AdoptionInvalid):policy(root,recipe,recipe.source)

@pytest.mark.parametrize('change', ['valid', 'missing', 'role', 'portable', 'binary_refusal'])
def test_prepared_style_background_is_qualified_and_retained_for_gc(change):
    recipe, root, args = closure_fixture()
    background = ArtifactRef(artifact_id=uuid4(), sha256=sha256(b'background').hexdigest())
    portable = str(background.artifact_id) if change != 'portable' else 'foreign-portable-name'
    style_body = canonical_bytes({'extensions': {'background': {'mode': 'asset', 'asset_id': portable}}})
    recipe.base_style.sha256 = sha256(style_body).hexdigest()
    root_body = canonical_bytes(recipe)
    root.sha256 = sha256(root_body).hexdigest()
    read = args['read_document']
    args['read_document'] = lambda ref, limit: (root_body if ref.artifact_id == root.artifact_id else
        style_body if ref.artifact_id == recipe.base_style.artifact_id else read(ref, limit))
    if change != 'missing':
        args['entries'].append((background, 'EncodedImage' if change == 'role' else 'StyleBackgroundAsset', 'image/png'))
    binary_calls = []
    def binary(ref, contract, binding):
        binary_calls.append((ref, contract))
        return change != 'binary_refusal'
    args['qualify_binary'] = binary
    qualifier = PreparedRecipeClosureQualifier(**args)
    if change == 'valid':
        assert qualifier(root, recipe, recipe.source) is True
        assert binary_calls == [(background, 'StyleBackgroundAsset')]
        retained = qualifier.qualified_document_refs(root, recipe.source)
        assert any(item['artifact_id'] == str(background.artifact_id)
                   and item['sha256'] == background.sha256 for item in retained)
    else:
        with pytest.raises(AdoptionInvalid):
            qualifier(root, recipe, recipe.source)
        with pytest.raises(AdoptionInvalid, match='RECEIPT_REQUIRED'):
            qualifier.qualified_document_refs(root, recipe.source)


@pytest.mark.parametrize('change', ['valid', 'missing', 'role', 'portable', 'binary_refusal'])
def test_prepared_override_background_is_qualified_and_retained_for_gc(change):
    recipe, root, args = closure_fixture()
    background = ArtifactRef(artifact_id=uuid4(), sha256=sha256(b'background').hexdigest())
    portable = str(background.artifact_id) if change != 'portable' else 'foreign-portable-name'
    override_body = canonical_bytes({'modules': {'extensions': {'background': {'mode': 'asset', 'asset_id': portable}}}})
    override_ref = recipe.overrides[0]
    override_ref.sha256 = sha256(override_body).hexdigest()
    root_body = canonical_bytes(recipe)
    root.sha256 = sha256(root_body).hexdigest()
    read = args['read_document']
    args['read_document'] = lambda ref, limit: (root_body if ref.artifact_id == root.artifact_id else
        override_body if ref.artifact_id == override_ref.artifact_id else read(ref, limit))
    if change != 'missing':
        args['entries'].append((background, 'EncodedImage' if change == 'role' else 'StyleBackgroundAsset', 'image/png'))
    binary_calls, occurrences = [], []
    def binary(ref, contract, binding):
        binary_calls.append((ref, contract))
        return change != 'binary_refusal'
    def occurrence(value):
        if value['reference'].artifact_id == background.artifact_id:
            occurrences.append((value['parent_contract'], value['path']))
        return True
    args['qualify_binary'] = binary
    args['qualify_occurrence'] = occurrence
    qualifier = PreparedRecipeClosureQualifier(**args)
    if change == 'valid':
        assert qualifier(root, recipe, recipe.source) is True
        assert binary_calls == [(background, 'StyleBackgroundAsset')]
        assert occurrences == [('Override', ('modules', 'extensions', 'background', 'asset_id'))]
        retained = qualifier.qualified_document_refs(root, recipe.source)
        assert any(item['artifact_id'] == str(background.artifact_id)
                   and item['sha256'] == background.sha256 for item in retained)
    else:
        with pytest.raises(AdoptionInvalid):
            qualifier(root, recipe, recipe.source)
        with pytest.raises(AdoptionInvalid, match='RECEIPT_REQUIRED'):
            qualifier.qualified_document_refs(root, recipe.source)


def test_stats_params_children_are_typed_from_exact_schema():
    from photo_worker.v2.interchange_recipe_closure import prepared_child_contract
    assert prepared_child_contract('Params', 'StatsParams', ('image',)) == 'WorkingImage'
    assert prepared_child_contract('Params', 'StatsParams', ('person', 'artifact')) == 'MaskPixels'
    assert prepared_child_contract('Params', 'StatsParams', ('person', 'transform_chain', 'steps', '*', 'inverse_field')) == 'DenseWarpField'
    with pytest.raises(AdoptionInvalid):
        prepared_child_contract('Params', 'StatsParams', ('uninstalled',))


def test_override_inline_children_have_closed_roles():
    from photo_worker.v2.interchange_recipe_closure import prepared_child_contract
    assert prepared_child_contract('Override', 'Override', ('mask_refs', '*', 'mask', 'artifact')) == 'MaskPixels'
    assert prepared_child_contract('Override', 'Override', ('mask_refs', '*', 'mask', 'transform_chain', 'steps', '*', 'inverse_field')) == 'DenseWarpField'
    assert prepared_child_contract('Override', 'Override', ('manual_landmarks', 'transform_chain', 'steps', '*', 'inverse_field')) == 'DenseWarpField'
    with pytest.raises(AdoptionInvalid):
        prepared_child_contract('Override', 'Override', ('mask_refs', '*', 'unknown'))
