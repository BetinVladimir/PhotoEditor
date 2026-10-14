# V32 head DRR single-pass source change — 2026-10-13

`CapacityDRR.nextHead` now examines every SQL-compressed head once per reservation and retains the lowest eligible tenant after the cursor plus the wrap candidate for each class. It then applies the existing class cursor order. This removes repeated full-head traversal for each class/pass while keeping FIFO, deficit, heavy-queue and tenant-wrap selection rules. An authored unit case compares it with the previous `Next` selector for all class cursors, several tenant cursors and reversed input order; the existing 60-cycle parity case remains.

Tests/builds/formatters were NOT RUN in the agreed code-first phase. The SQL head snapshot and the Go `Heads` slice still grow with active tenant/class count. This change improves selection work but does **not** close bounded-memory scheduler acceptance or the remaining V32 runtime/SQL gates.
