package maintenance

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type authoredSQLABI struct {
	Names    []string `json:"names"`
	Types    []string `json:"types"`
	Modes    []string `json:"modes"`
	Returns  string   `json:"returns"`
	SetOf    bool     `json:"setof"`
	Defaults int      `json:"defaults"`
	Variadic bool     `json:"variadic"`
}

// This is a post-install contract, independent of migration filenames or ledger
// rows. Keep the fixture synchronized only with reviewed forward successors.
func TestAuthoredFaceSkinInstalledFunctionContracts(t *testing.T) {
	raw, err := os.ReadFile("testdata/authored-face-skin-sql-contracts.json")
	if err != nil {
		t.Fatal(err)
	}
	var contracts []struct {
		Name     string         `json:"name"`
		BodySHA  string         `json:"body_sha256"`
		Language string         `json:"language"`
		Roles    []string       `json:"execute_roles"`
		ABI      authoredSQLABI `json:"abi"`
	}
	if err = json.Unmarshal(raw, &contracts); err != nil || len(contracts) != 33 {
		t.Fatal("invalid complete authored SQL contract fixture", err)
	}
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated fully migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, contract := range contracts {
		t.Run(contract.Name, func(t *testing.T) {
			var count int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname=$1`, contract.Name).Scan(&count); err != nil || count != 1 {
				t.Fatal("required unique function missing or overloaded", count, err)
			}
			var abiRaw []byte
			if err := db.QueryRow(ctx, `SELECT jsonb_build_object('names',p.proargnames,
                'types',ARRAY(SELECT ns.nspname::text||'.'||t.typname::text FROM unnest(coalesce(p.proallargtypes,p.proargtypes::oid[])) WITH ORDINALITY a(oid,ord) JOIN pg_catalog.pg_type t ON t.oid=a.oid JOIN pg_catalog.pg_namespace ns ON ns.oid=t.typnamespace ORDER BY a.ord),
                'modes',coalesce(p.proargmodes::text[],array_fill('i'::text,ARRAY[p.pronargs::int])),
                'returns',(SELECT ns.nspname::text||'.'||t.typname::text FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace ns ON ns.oid=t.typnamespace WHERE t.oid=p.prorettype),
                'setof',p.proretset,'defaults',p.pronargdefaults,'variadic',p.provariadic<>0)
                FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname=$1`, contract.Name).Scan(&abiRaw); err != nil {
				t.Fatal(err)
			}
			var abi authoredSQLABI
			if err := json.Unmarshal(abiRaw, &abi); err != nil || !reflect.DeepEqual(abi, contract.ABI) {
				t.Fatalf("installed function ABI drift: got=%+v want=%+v err=%v", abi, contract.ABI, err)
			}

			var bodySHA, owner, language string
			var definer, ownerLogin, aclInvalid bool
			var config, roles []string
			err := db.QueryRow(ctx, `SELECT pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(p.prosrc,'UTF8')),'hex'),r.rolname,l.lanname,p.prosecdef,r.rolcanlogin,p.proconfig,
   ARRAY(SELECT CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE g.rolname::text END FROM pg_catalog.aclexplode(coalesce(p.proacl,pg_catalog.acldefault('f',p.proowner))) a LEFT JOIN pg_catalog.pg_roles g ON g.oid=a.grantee ORDER BY 1),
   EXISTS(SELECT FROM pg_catalog.aclexplode(coalesce(p.proacl,pg_catalog.acldefault('f',p.proowner))) a WHERE a.grantor<>p.proowner OR a.is_grantable OR a.privilege_type<>'EXECUTE')
   FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace JOIN pg_catalog.pg_roles r ON r.oid=p.proowner JOIN pg_catalog.pg_language l ON l.oid=p.prolang WHERE n.nspname='photo' AND p.proname=$1`, contract.Name).Scan(&bodySHA, &owner, &language, &definer, &ownerLogin, &config, &roles, &aclInvalid)
			if err != nil {
				t.Fatal(err)
			}
			if bodySHA != contract.BodySHA || owner != "photo_fn_owner" || ownerLogin || !definer || language != contract.Language || !reflect.DeepEqual(config, []string{"search_path=pg_catalog, photo"}) || !reflect.DeepEqual(roles, contract.Roles) || aclInvalid {
				t.Fatalf("installed authored function contract drift: owner=%s language=%s definer=%v ownerLogin=%v config=%v roles=%v ACLInvalid=%v bodySHA=%s", owner, language, definer, ownerLogin, config, roles, aclInvalid, bodySHA)
			}
		})
	}
}
