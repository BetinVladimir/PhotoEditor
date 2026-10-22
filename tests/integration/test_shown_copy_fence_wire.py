"""Real SQL lossless fence boundary vectors; one private DB per pure matrix."""
import json

VALID_WIRES = ((1, 1), (9007199254740991, 9007199254740991),
               ('9007199254740992', 9007199254740992), ('9223372036854775807', 9223372036854775807))
INVALID_WIRES = (None, True, 0, -1, 1.0, '1', '01', 9007199254740992,
                 '9223372036854775808', '+9007199254740992', '９００７１９９２５４７４０９９２', {}, [])


def test_sql_copy_fence_decodes_exact_boundaries(db):
    for index, (wire, expected) in enumerate(VALID_WIRES):
        row = db.execute('SELECT photo.shown_copy_fence_wire_value(%s::jsonb)', (json.dumps(wire),)).fetchone()
        assert row[0] == expected, f'valid-fence-{index}: {wire!r}'


def test_sql_copy_fence_rejects_ambiguous_or_unsafe_wire(db):
    for index, wire in enumerate(INVALID_WIRES):
        row = db.execute('SELECT photo.shown_copy_fence_wire_value(%s::jsonb)', (json.dumps(wire),)).fetchone()
        assert row[0] is None, f'invalid-fence-{index}: {wire!r}'
