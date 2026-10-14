from pathlib import Path
import re
import pytest

ROOT=Path(__file__).parents[2]


def assert_atomic_dollar_blocks(up, name):
    active = False
    quote = None
    for line in up.splitlines():
        marker = line.strip()
        if marker == '-- +goose StatementBegin':
            assert not active and quote is None, name
            active = True
        elif marker == '-- +goose StatementEnd':
            assert active and quote is None, name
            active = False
        elif not marker.startswith('--'):
            for token in re.findall(r'\$[A-Za-z_][A-Za-z_0-9]*\$|\$\$', line):
                if quote is None:
                    assert active, name
                    quote = token
                elif token == quote:
                    quote = None
    assert not active and quote is None, name


def test_every_dollar_quoted_up_statement_is_atomic_for_goose():
    for migration in sorted((ROOT/'backend/db/migrations').glob('*.sql')):
        up=migration.read_text().split('-- +goose Down')[0]
        assert_atomic_dollar_blocks(up, migration.name)


def test_multiple_functions_can_share_one_goose_statement_block():
    assert_atomic_dollar_blocks('-- +goose StatementBegin\nDO $$ BEGIN NULL; END $$;\nDO $body$ BEGIN NULL; END $body$;\n-- +goose StatementEnd', 'grouped')


@pytest.mark.parametrize('sql', [
    'DO $$ BEGIN NULL; END $$;',
    '-- +goose StatementBegin\nDO $$ BEGIN NULL; END $$;',
    '-- +goose StatementBegin\nDO $$ BEGIN NULL;\n-- +goose StatementEnd',
    '-- +goose StatementEnd',
])
def test_invalid_goose_boundaries_fail(sql):
    with pytest.raises(AssertionError):
        assert_atomic_dollar_blocks(sql, 'invalid')
