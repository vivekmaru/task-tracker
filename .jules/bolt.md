## 2025-05-18 - Missing Index on Rapidly Growing Table
**Learning:** Found a missing index on `attempt_checkpoints(ticket_id)` in the initial schema. The query `ListAttemptCheckpointsByTicket` was filtering by `ticket_id` and ordering by `created_at` without a supporting index. Since `attempt_checkpoints` is an append-only table that grows rapidly as agents report progress, this would cause severe performance degradation (full table scans) when humans view ticket details in the TUI.
**Action:** Always verify that frequently accessed append-only tables, especially those queried for timelines or queues, have composite indexes covering both the foreign key filter and the sort column (e.g., `ticket_id`, `created_at`).

## 2025-06-29 - Updating Indexes When Sort Columns Change
**Learning:** Found missing indexes on `ticket_events` after the sort column for timelines was migrated from `created_at` to `event_sequence`. The original composite indexes `(ticket_id, created_at)` and `(attempt_id, created_at)` were not updated, leading to queries like `ListTicketEventsByTicket` performing full table sorts when ordering by `event_sequence`.
**Action:** Always verify and update composite indexes when migrating the sort column of frequently accessed append-only tables to maintain fast pagination and timeline retrieval.
