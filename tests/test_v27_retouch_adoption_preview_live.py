"""Preserve every existing adoption gate while adding preview cancellation."""
from pathlib import Path


def test_retouch_adoption_preview_liveness_preserves_previous_definition():
    migrations=Path(__file__).resolve().parents[1]/'backend/db/migrations'
    previous=next(migrations.glob('02385*')).read_text()
    current=(migrations/'02397_v27_shown_retouch_adoption_preview_live.sql').read_text()
    def definition(text):
        start=text.index('FUNCTION photo.v27_shown_retouch_adoption_ready(')
        return text[start:text.index('-- +goose StatementEnd',start)]
    restored=definition(current).replace(
        "OR NOT old_preview.graph_sealed OR old_preview.cancel_requested\n OR old_preview.status NOT IN('running','completed') OR old_preview.created_by",
        "OR NOT old_preview.graph_sealed OR old_preview.created_by").replace(
        "IF NOT FOUND OR producer.state IS DISTINCT FROM 'succeeded' OR producer.cancel_requested",
        "IF NOT FOUND OR producer.state IS DISTINCT FROM 'succeeded'")
    assert restored==definition(previous)
    assert 'OR old_preview.cancel_requested' in current
    assert "old_preview.status NOT IN('running','completed')" in current
    assert "producer.state IS DISTINCT FROM 'succeeded' OR producer.cancel_requested" in current
