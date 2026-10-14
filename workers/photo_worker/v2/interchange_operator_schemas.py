"""Owned, bounded operation identities resolved only against pinned definitions."""


def build_interchange_operator_schemas(definitions, supplied=None):
    defaults = {('portrait.hair', '1'): 'PortraitHairParams',
                ('portrait.clothing', '1'): 'PortraitClothingParams',
                ('image.denoise', '1'): 'DenoiseParams',
                ('image.upscale', '1'): 'UpscaleParams',
                ('color.stats', '1'): 'StatsParams'}
    if not isinstance(definitions, dict):
        raise ValueError('INTERCHANGE_CLOSURE_SCHEMAS')
    if supplied is None:
        return defaults
    if not isinstance(supplied, dict):
        raise ValueError('INTERCHANGE_CLOSURE_OPERATORS')
    def identifier(value, maximum):
        if not isinstance(value, str) or not value or '\x00' in value:
            return False
        try:
            return len(value.encode('utf-8')) <= maximum
        except UnicodeEncodeError:
            return False
    for key, schema in supplied.items():
        if (not isinstance(key, tuple) or len(key) != 2
                or not identifier(key[0], 80) or not identifier(key[1], 20)
                or not identifier(schema, 120) or schema not in definitions
                or not isinstance(definitions[schema], (dict, bool))
                or key in defaults and defaults[key] != schema):
            raise ValueError('INTERCHANGE_CLOSURE_OPERATORS')
    defaults.update(supplied)
    if len(defaults) > 256:
        raise ValueError('INTERCHANGE_CLOSURE_OPERATORS')
    return defaults
