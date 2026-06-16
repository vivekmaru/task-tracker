## 2025-05-18 - Missing Index on Rapidly Growing Table
**Learning:** Found a missing index on `attempt_checkpoints(ticket_id)` in the initial schema. The query `ListAttemptCheckpointsByTicket` was filtering by `ticket_id` and ordering by `created_at` without a supporting index. Since `attempt_checkpoints` is an append-only table that grows rapidly as agents report progress, this would cause severe performance degradation (full table scans) when humans view ticket details in the TUI.
**Action:** Always verify that frequently accessed append-only tables, especially those queried for timelines or queues, have composite indexes covering both the foreign key filter and the sort column (e.g., `ticket_id`, `created_at`).

## 2025-05-18 - Missing Indexes on Sorted Column
**Learning:** Found that when a query's `ORDER BY` clause was updated to use a new sequence column (`event_sequence` instead of `created_at`), the existing composite indexes (`ticket_id, created_at` and `attempt_id, created_at`) were no longer able to optimize the sort operation, leading to potential performance issues on large datasets.
**Action:** When modifying the sort column of frequently queried tables, always ensure that corresponding composite indexes (filtering columns + sort column) are updated or added to prevent expensive sort operations.
