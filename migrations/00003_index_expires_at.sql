-- +goose Up
-- The cleanup job finds expired pastes through this index, so it reads only
-- them instead of the whole table. Pastes that never expire are NULL here, and
-- the job's range, expires_at <= now, never reaches them.
-- An index can't be added INSTANT. INPLACE with LOCK = NONE builds it while
-- reads and writes go on. If MySQL can't do that, the migration fails instead
-- of blocking the table while it builds.

CREATE INDEX idx_pastes_expires_at ON pastes (expires_at)
    ALGORITHM = INPLACE LOCK = NONE;
