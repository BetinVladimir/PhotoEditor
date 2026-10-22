"""Forward-only media repair must preserve every prior authority gate and ACL."""
from pathlib import Path


def test_shown_media_verification_repair_preserves_previous_definitions():
    migrations = Path(__file__).resolve().parents[1] / 'backend/db/migrations'
    previous = (migrations / '02388_v27_shown_retouch_media_sources.sql').read_text()
    current = (migrations / '02398_v27_shown_media_verified_sources.sql').read_text()

    def definitions(text):
        return text[text.index('-- +goose StatementBegin'):]

    restored = current.replace(
        '\n  IF photo.semantic_shown_media_source_live(p_event,expected) IS DISTINCT FROM true THEN RETURN false;END IF;', '')
    restored = restored.replace('expected jsonb;ref jsonb;', 'expected jsonb;')
    start = restored.index('  -- An exact reference is insufficient')
    end = restored.index('  END LOOP;', start) + len('  END LOOP;')
    restored = restored[:start-1] + restored[end:]
    assert definitions(restored) == definitions(previous)
    assert "a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0" in current
    assert "a.object_version NOT IN('','null')" in current
    assert 'a.expires_at>clock_timestamp()' in current
    assert "a.sha256=ref->>'sha256'" in current
