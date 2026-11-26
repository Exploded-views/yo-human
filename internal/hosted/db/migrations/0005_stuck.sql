-- Hosted mode, schema 5: where a run of transient nudge failures began, and its latest status
-- (review of Task 11). A 2xx clears both. DeliveryState counts a run older than 10 minutes.
ALTER TABLE nudge_state ADD COLUMN stuck_since  TEXT NOT NULL DEFAULT '';
ALTER TABLE nudge_state ADD COLUMN stuck_status INTEGER NOT NULL DEFAULT 0;
