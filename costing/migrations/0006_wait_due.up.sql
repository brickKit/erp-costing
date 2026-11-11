-- A wait's period (VS03, LY01): waiting_since is when the wait began (the recovery check erp.costing.waiting reports
-- it after an hour), wait_due when the current period ends; a gap read back without the missing number waits another
-- period, so only wait_due moves. A scope waiting now is due at once (its next run reads back and starts a period).
ALTER TABLE scope ADD COLUMN wait_due timestamptz;
UPDATE scope SET wait_due = waiting_since WHERE waiting_since IS NOT NULL;
