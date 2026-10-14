import pytest
from photo_worker.v2.interchange_operator_schemas import build_interchange_operator_schemas


def test_operator_registry_owns_configuration_and_preserves_builtins():
    supplied = {('image.denoise','1'):'DenoiseParams'}
    result = build_interchange_operator_schemas({'DenoiseParams':{}}, supplied)
    supplied.clear()
    assert result[('image.denoise','1')] == 'DenoiseParams'
    assert result[('portrait.hair','1')] == 'PortraitHairParams'
    assert build_interchange_operator_schemas({'DenoiseParams':{}})[('image.denoise','1')] == 'DenoiseParams'


@pytest.mark.parametrize('operation', ['a'*81, '\ud800', 'bad\x00name', '', None])
def test_operator_identifiers_are_bounded_utf8(operation):
    with pytest.raises(ValueError, match='OPERATORS'):
        build_interchange_operator_schemas({'Params':{}}, {(operation,'1'):'Params'})


def test_registry_limit_includes_builtin_operations():
    supplied = {(f'custom.{i}','1'):'Params' for i in range(251)}
    assert len(build_interchange_operator_schemas({'Params':{}}, supplied)) == 256
    supplied[('custom.extra','1')] = 'Params'
    with pytest.raises(ValueError, match='OPERATORS'):
        build_interchange_operator_schemas({'Params':{}}, supplied)


@pytest.mark.parametrize('schema', [None, [], 'not-a-schema', 1])
def test_registry_refuses_invalid_schema_definition(schema):
    with pytest.raises(ValueError, match='OPERATORS'):
        build_interchange_operator_schemas({'Params':schema}, {('custom','1'):'Params'})


@pytest.mark.parametrize('schema', ['PortraitHairParams', 'Params'])
def test_denoise_builtin_cannot_be_rebound_to_another_params_schema(schema):
    definitions = {'DenoiseParams': {}, 'PortraitHairParams': {}, 'Params': {}}
    with pytest.raises(ValueError, match='OPERATORS'):
        build_interchange_operator_schemas(definitions, {('image.denoise', '1'): schema})


def test_stats_builtin_has_exact_schema_and_cannot_be_rebound():
    assert build_interchange_operator_schemas({})[('color.stats', '1')] == 'StatsParams'
    with pytest.raises(ValueError, match='OPERATORS'):
        build_interchange_operator_schemas({'Params': {}}, {('color.stats', '1'): 'Params'})
