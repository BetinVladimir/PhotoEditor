from pathlib import Path

ROOT=Path(__file__).parents[2]

def test_every_dollar_quoted_up_statement_is_atomic_for_goose():
    for migration in sorted((ROOT/'backend/db/migrations').glob('*.sql')):
        up=migration.read_text().split('-- +goose Down')[0]
        dollar_blocks=up.count('$$')//2
        if dollar_blocks:
            assert up.count('-- +goose StatementBegin')==dollar_blocks,migration.name
            assert up.count('-- +goose StatementEnd')==dollar_blocks,migration.name
