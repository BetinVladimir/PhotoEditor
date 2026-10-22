"""Authored policy/ACL scenarios; full owner+guest production path remains runtime qualification."""
import pytest
from psycopg.types.json import Jsonb


def test_download_policy_defaults_and_closed_bounds(db):
    assert db.execute("SELECT photo.download_link_seconds('{}'::jsonb),photo.download_policy_seconds('{}'::jsonb,'retention_seconds')").fetchone() == (60,86400)
    config={"download_policy":{"retention_seconds":3600,"link_seconds":15}}
    assert db.execute("SELECT photo.download_link_seconds(%s),photo.download_policy_seconds(%s,'retention_seconds')",(Jsonb(config),Jsonb(config))).fetchone()==(15,3600)


@pytest.mark.parametrize('policy',[
    None,{}, {'link_seconds':15},
    {'link_seconds':61,'retention_seconds':86400},
    {'link_seconds':0,'retention_seconds':86400},
    {'link_seconds':'15','retention_seconds':86400},
    {'link_seconds':15,'retention_seconds':86400,'unknown':True},
])
def test_download_policy_invalid_is_never_defaulted(db,policy):
    with pytest.raises(Exception):
        with db.transaction():
            db.execute("SELECT photo.download_link_seconds(%s)",(Jsonb({'download_policy':policy}),)).fetchone()


def test_download_policy_signer_authority_acl(db):
    signature='photo.download_intent_signing_seconds(bytea,text,text,text,text,bigint,text)'
    assert db.execute("SELECT has_function_privilege('photo_portal',%s,'EXECUTE'),has_function_privilege('photo_api',%s,'EXECUTE'),has_function_privilege('photo_worker',%s,'EXECUTE')",(signature,signature,signature)).fetchone()==(True,False,False)
    assert db.execute("SELECT photo.download_intent_signing_seconds(decode(repeat('00',32),'hex'),'photo','missing','v',''::text,1,'image/jpeg')").fetchone()[0] is None
