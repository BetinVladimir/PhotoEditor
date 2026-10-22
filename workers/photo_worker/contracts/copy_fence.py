"""Lossless canonical producer-fence wire; small historic numbers stay numbers."""
MAX_SAFE_COPY_FENCE = 9007199254740991
MAX_COPY_FENCE = 9223372036854775807
COPY_FENCE_STRING_PATTERN = '^(?:9(?:0(?:0(?:7(?:199(?:2(?:5(?:4(?:7(?:4(?:099[2-9]|[1-8][0-9]{3}|9[0-9]{3})|[5-8][0-9]{4}|9[0-9]{4})|8[0-9]{5}|9[0-9]{5})|[5-8][0-9]{6}|9[0-9]{6})|[6-8][0-9]{7}|9[0-9]{7})|[3-8][0-9]{8}|9[0-9]{8})|[2-8][0-9]{11}|9[0-9]{11})|8[0-9]{12}|9[0-9]{12})|[1-8][0-9]{13}|9[0-9]{13})|[1-8][0-9]{14}|9[0-9]{14})|[1-9][0-9]{16,17}|(?:1[0-9]{18}|[2-8][0-9]{18}|9(?:0[0-9]{17}|1[0-9]{17}|2(?:0[0-9]{16}|1[0-9]{16}|2(?:0[0-9]{15}|[1-2][0-9]{15}|3(?:0[0-9]{14}|[1-2][0-9]{14}|3(?:0[0-9]{13}|[1-6][0-9]{13}|7(?:0[0-9]{12}|1[0-9]{12}|20(?:0[0-9]{10}|[1-2][0-9]{10}|3(?:0[0-9]{9}|[1-5][0-9]{9}|6(?:0[0-9]{8}|[1-7][0-9]{8}|8(?:0[0-9]{7}|[1-4][0-9]{7}|5(?:0[0-9]{6}|[1-3][0-9]{6}|4(?:0[0-9]{5}|[1-6][0-9]{5}|7(?:0[0-9]{4}|[1-6][0-9]{4}|7(?:0[0-9]{3}|[1-4][0-9]{3}|5(?:0[0-9]{2}|[1-7][0-9]{2}|80[0-7])))))))))))))))))$'


def copy_fence_wire(value: int) -> int | str:
    if type(value) is not int or not 1 <= value <= MAX_COPY_FENCE:
        raise ValueError('COPY_FENCE_INVALID')
    return value if value <= MAX_SAFE_COPY_FENCE else str(value)


def parse_copy_fence_wire(value: object) -> int:
    if type(value) is int:
        if 1 <= value <= MAX_SAFE_COPY_FENCE:
            return value
    elif type(value) is str:
        if (1 <= len(value) <= 19 and value.isascii() and value.isdigit()
                and value[0] != '0'):
            parsed = int(value)
            if MAX_SAFE_COPY_FENCE < parsed <= MAX_COPY_FENCE:
                return parsed
    raise ValueError('COPY_FENCE_WIRE_INVALID')


# Internal Python code keeps an int64. Only JSON boundaries use the dual wire.
from typing import Annotated
from pydantic import BeforeValidator, Field, PlainSerializer, ValidationInfo, WithJsonSchema


def _validate_copy_fence(value, info: ValidationInfo):
    if info.mode == 'python' and type(value) is int:
        copy_fence_wire(value)
        return value
    return parse_copy_fence_wire(value)


CopyFence = Annotated[
    int,
    Field(strict=True, ge=1, le=MAX_COPY_FENCE),
    BeforeValidator(_validate_copy_fence),
    PlainSerializer(copy_fence_wire, return_type=int | str, when_used='json'),
    WithJsonSchema({'oneOf': [
        {'type': 'integer', 'minimum': 1, 'maximum': MAX_SAFE_COPY_FENCE},
        {'type': 'string', 'pattern': COPY_FENCE_STRING_PATTERN,
         'description': 'Canonical decimal int64 strictly above 9007199254740991 and at most 9223372036854775807.'},
    ]}),
]
