# Packaged contracts

`endpoints.json` is the canonical current method/path union. Each row states its
`origin` and `contract_format`:

- `executable_schema`: V2-governed request/response/query names resolve in
  `docs/implementation-v2/contracts/schemas.json`. For overlapping identities,
  this current executable projection takes precedence over the original types.
- `legacy_typed_source`: the exact native route/service/type declaration is kept
  under `typed_source_contract`; executable schema fields are null. These names
  are normative documentation and must not be treated as permissive schemas or
  silently guessed aliases. This format does not claim complete executable
  baseline DTO coverage.

`baseline-endpoints.json` separately retains the native `format_version` baseline
registry. `v2-endpoints.json` retains the native executable V2 registry. The
contract generation source packages these explicit sources, not a previous
packaged mirror. It refuses conflicting duplicate identities, parameter aliases
and unresolved executable schema names before writing outputs.

The current canonical artifact was manually authored during source-only work.
Generation, drift checks and runtime verification have not been executed.
