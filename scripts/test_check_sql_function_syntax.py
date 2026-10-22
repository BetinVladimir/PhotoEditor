import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location(
    "sql_syntax", Path(__file__).with_name("check-sql-function-syntax.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class FunctionBodySyntaxTests(unittest.TestCase):
    def test_rejects_invalid_dollar_quoted_sql_body(self):
        with self.assertRaises(Exception):
            module.check("CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT FROM $$;")

    def test_rejects_invalid_plpgsql_body(self):
        with self.assertRaises(Exception):
            module.check("CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN ; END $$;")

    def test_rejects_invalid_do_body(self):
        with self.assertRaises(Exception):
            module.check("DO $$ BEGIN IF true THEN NULL; END $$;")

    def test_non_ascii_before_function_uses_byte_offsets(self):
        result = module.check("-- фотография\nSELECT 1;\nCREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;")
        self.assertEqual(result, [{"name": "f", "language": "plpgsql", "body_checked": True}])

    def test_sql_standard_body_and_multiple_statements(self):
        result = module.check("CREATE FUNCTION f() RETURNS int LANGUAGE sql RETURN 1; SELECT 2;")
        self.assertEqual(len(result), 1)

    def test_goose_up_excludes_unexecuted_down(self):
        result = module.check_goose_up("-- +goose Up\nSELECT 1;\n-- +goose Down\nDO $$ BEGIN RETURN false; END $$;")
        self.assertEqual(result, [])

    def test_goose_up_rejects_duplicate_or_reversed_directives(self):
        for source in ("-- +goose Up\n-- +goose Up\nSELECT 1;",
                       "-- +goose Down\n-- +goose Up\nSELECT 1;"):
            with self.subTest(source=source), self.assertRaises(ValueError):
                module.check_goose_up(source)


if __name__ == "__main__":
    unittest.main()
