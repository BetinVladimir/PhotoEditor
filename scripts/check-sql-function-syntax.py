#!/usr/bin/env python3
"""Parse SQL plus SQL/PLpgSQL function bodies without starting PostgreSQL.

Requires pglast built for the target PostgreSQL major version. This checks syntax,
not database name/type resolution, permissions, dynamic SQL, or runtime behavior.
"""
import argparse
import json
import re
from pathlib import Path

from pglast.parser import parse_plpgsql_json, parse_sql_json


def check(text):
    statements = json.loads(parse_sql_json(text))["stmts"]
    functions = []
    for item in statements:
        node = item["stmt"]
        if "DoStmt" in node:
            # The parser accepts the complete DO statement, not just its body.
            start = item.get("stmt_location", 0)
            size = item.get("stmt_len", len(text.encode()) - start)
            statement = text.encode()[start:start + size].decode()
            options = {x["DefElem"]["defname"]: x["DefElem"].get("arg")
                       for x in node["DoStmt"]["args"]}
            language = options.get("language", {"String": {"sval": "plpgsql"}})
            if language["String"]["sval"] == "plpgsql":
                parse_plpgsql_json(statement + ";")
            continue
        function = node.get("CreateFunctionStmt")
        if function is None:
            continue
        options = {x["DefElem"]["defname"]: x["DefElem"].get("arg")
                   for x in function.get("options", [])}
        language = options.get("language", {}).get("String", {}).get("sval")
        name = ".".join(x["String"]["sval"] for x in function["funcname"])
        body = options.get("as")
        if language == "sql" and body:
            # Outer CREATE parsing does not validate the dollar-quoted SQL body.
            parts = body["List"]["items"]
            if len(parts) != 1:
                raise ValueError(f"Unexpected SQL body representation: {name}")
            parse_sql_json(parts[0]["String"]["sval"])
        elif language == "plpgsql":
            start = item.get("stmt_location", 0)
            size = item.get("stmt_len", len(text.encode()) - start)
            parse_plpgsql_json(text.encode()[start:start + size].decode() + ";")
        functions.append({"name": name, "language": language,
                          "body_checked": language in ("sql", "plpgsql")})
    return functions


def check_goose_up(text):
    # Match complete directive lines; never split on a bare word in a body.
    up = list(re.finditer(r"(?m)^-- \+goose Up[ \t]*\r?$", text))
    down = list(re.finditer(r"(?m)^-- \+goose Down[ \t]*\r?$", text))
    if len(up) != 1 or len(down) > 1 or (down and down[0].start() < up[0].end()):
        raise ValueError("Ambiguous Goose Up/Down directives")
    end = down[0].start() if down else len(text)
    return check(text[up[0].end():end])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--goose-up", action="store_true", help="Check only the forward migration section")
    parser.add_argument("files", nargs="+", type=Path)
    args = parser.parse_args()
    results = []
    for path in args.files:
        results.append({"path": str(path), "functions": (check_goose_up if args.goose_up else check)(path.read_text())})
    print(json.dumps({"syntax_only": True, "results": results}, indent=2))


if __name__ == "__main__":
    main()
