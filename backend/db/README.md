# Migration source admission

`ValidateBundle` admits each numbered migration source with a fixed **64 MiB (67,108,864 bytes)** ceiling. This is a deployment input and memory budget, not permission to execute SQL or an alternative to source/vector, catalog-CAS, ACL, independent-review or installed-security checks.

The former 16 MiB implementation ceiling rejected the reviewed head2616 migration of 27,752,180 bytes before Goose. The new ceiling provides more than twice that observed source size. It is deliberately finite and cannot be changed by environment or caller configuration. Future catalog growth beyond this ceiling requires a separately reviewed transport change; never remove/truncate capture guards or silently accept larger sources.

The reader opens the file, rejects nonregular/empty/oversized Stat metadata, then reads at most the ceiling plus one byte. The extra byte distinguishes an exact-limit source from a growing or dishonest source. A reported size within budget does not authorize a larger stream. Read and close failures refuse admission without returning partial SQL or exposing filesystem error details. The reader closes the handle on every ordinary return path and does not use an FS-specific unbounded ReadFile shortcut. Resident allocation includes the bounded byte buffer, its growth capacity and the SQL string copy; this is not a total-process memory budget.

The baseline validator has no context/deadline for filesystem reads; this change does not add one. Migration sources are trusted local deployment artifacts, not remote streams. Existing owning process deadlines remain required. Goose's separate per-line scanner ceiling also remains in force; generated capture literals must use the reviewed bounded-leaf transport.

`validation_test.go` directly exercises the reusable bounded reader for exact boundary, size+1, dishonest Stat, empty/negative metadata, read error and close error. It asserts consumed byte bounds, closure and no partial result. A separate ValidateBundle regression uses an actual-sized 27,752,180-byte fixture. Direct helper tests do not depend on fs.ReadDirFS shortcuts; the bundle regression uses fstest.MapFS to cover directory enumeration and production wiring.

A successor deployment must bind the changed production source hashes into its immutable source view and rebuild the genuine `cmd/photo` issuer. Historical frozen source views and their reviewed hashes must not be edited. Source tests alone do not qualify a live migration or whole-module acceptance.
