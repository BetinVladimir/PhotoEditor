import os
import subprocess
from pathlib import Path
from urllib.parse import quote
from uuid import uuid4

import psycopg
from psycopg import sql
from psycopg.conninfo import make_conninfo


def test_goose_empty_database_and_repeat_are_stable(database):
    name='photo_goose_'+uuid4().hex
    admin={key:database[key] for key in ('host','port','user','password') if key in database}
    with psycopg.connect(**admin,dbname='postgres',autocommit=True) as conn:
        conn.execute(sql.SQL('CREATE DATABASE {}').format(sql.Identifier(name)))
    try:
        dsn=make_conninfo(**admin,dbname=name)
        env={**os.environ,'PHOTO_ROLE':'migrate','DATABASE_URL':dsn,
             'APP_ENV':'development','GOCACHE':str((Path(__file__).resolve().parents[2]/'work/go-cache'))}
        command=['go','run','./cmd/photo']
        first=subprocess.run(command,cwd='backend',env=env,capture_output=True,text=True,timeout=180)
        assert first.returncode==0,first.stderr[-2000:]
        second=subprocess.run(command,cwd='backend',env=env,capture_output=True,text=True,timeout=180)
        assert second.returncode==0,second.stderr[-2000:]
        with psycopg.connect(**admin,dbname=name) as conn:
            version=conn.execute('SELECT max(version_id),count(*) FILTER (WHERE is_applied) FROM goose_db_version').fetchone()
            assert version[0]>=167 and version[1]>=167
            tables=conn.execute("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='photo' AND c.relkind='r'").fetchone()[0]
            assert tables>=65
            grants=conn.execute("SELECT has_function_privilege('photo_api','photo.create_tenant(uuid,uuid,uuid,text,jsonb,jsonb,text,text)','EXECUTE'),has_function_privilege('public','photo.create_tenant(uuid,uuid,uuid,text,jsonb,jsonb,text,text)','EXECUTE')").fetchone()
            assert grants==(True,False)
            for signature in (
                'photo.commit_semantic_review_close(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb)',
                'photo.semantic_close_replay(uuid,uuid,uuid,uuid,text,text)',
                'photo.semantic_review_status(uuid,uuid,uuid,uuid)',
                'photo.semantic_proposal_catalog(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid)',
                'photo.semantic_proposal_inventory(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid)',
                'photo.reserve_v27_semantic_intent_upload(uuid,uuid,uuid,uuid,uuid,text,bigint,text)',
                'photo.complete_v27_semantic_intent_upload(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)',
            ):
                privileges=conn.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE'),has_function_privilege('public',%s,'EXECUTE'),has_function_privilege('photo_worker',%s,'EXECUTE'),has_function_privilege('photo_scheduler',%s,'EXECUTE')", (signature,)*4).fetchone()
                assert privileges==(True,False,False,False)
                security=conn.execute("SELECT p.prosecdef,r.rolname,p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=%s::regprocedure", (signature,)).fetchone()
                assert security==(True,'photo_fn_owner',['search_path=pg_catalog, photo'])
            for signature in (
                'photo.lock_semantic_proposal_inventory(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid)',
                'photo.lock_semantic_proposal_document(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint)',
                'photo.lock_semantic_proposal_qualification(uuid,uuid,uuid,uuid,jsonb)',
            ):
                privileges=conn.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE'),has_function_privilege('public',%s,'EXECUTE'),has_function_privilege('photo_worker',%s,'EXECUTE'),has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)*4).fetchone()
                assert privileges==(False,False,False,True)
                security=conn.execute("SELECT p.prosecdef,r.rolname,p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=%s::regprocedure",(signature,)).fetchone()
                assert security==(True,'photo_fn_owner',['search_path=pg_catalog, photo'])
    finally:
        with psycopg.connect(**admin,dbname='postgres',autocommit=True) as conn:
            conn.execute(sql.SQL('DROP DATABASE IF EXISTS {} WITH (FORCE)').format(sql.Identifier(name)))
