from pathlib import Path
import hashlib,re
ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00195_contact_sheet_resources.sql').read_text()
FONT=ROOT/'backend/internal/contactsheet/NotoSans-Regular.ttf'
LICENSE=ROOT/'backend/internal/contactsheet/OFL-1.1.txt'

def test_font_is_pinned_and_licensed():
 assert hashlib.sha256(FONT.read_bytes()).hexdigest()=='b85c38ecea8a7cfb39c24e395a4007474fa5a4fc864f6ee33309eb4948d232d5'
 assert b'SIL OPEN FONT LICENSE Version 1.1' in LICENSE.read_bytes()

def test_resource_is_quota_accounted_and_contact_kind_is_closed():
 for value in ['CREATE TABLE photo.tenant_static_resources','reserved_bytes=reserved_bytes+p_bytes','storage_bytes=storage_bytes+p_bytes',"'contact_sheet_font_v1'","'contact_sheet'","'report.contact_sheet'"]:
  assert value in SQL
 assert "GRANT EXECUTE ON FUNCTION photo.reserve_contact_sheet_font(uuid,text,bigint,text) TO photo_api" in SQL
 assert "GRANT EXECUTE ON FUNCTION photo.complete_contact_sheet_font(uuid,uuid,uuid,text,bigint,text,text) TO photo_api" in SQL
 assert "RAISE EXCEPTION 'Forward-only migration'" in SQL

def test_contact_kind_extension_preserves_existing_interactive_kinds():
 match=re.search(r"interactive_tasks_kind_check\s+CHECK\(kind IN\(([^)]+)\)\)",SQL)
 assert match
 kinds=set(re.findall(r"'([^']+)'",match.group(1)))
 assert kinds=={'mask_manual','mask_prompt','generation','generation_preview','preview','contact_sheet'}
 assert "WHEN 'contact_sheet' THEN 'report.contact_sheet' ELSE 'editor.preview'" in SQL
