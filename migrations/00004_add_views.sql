-- +goose Up
-- How many times the paste was read, up to the app's last write of its counts.
-- INSTANT adds the column without rewriting the table. If MySQL can't do that,
-- the migration fails instead of rebuilding every row while the app waits.

ALTER TABLE pastes
    ADD COLUMN views BIGINT UNSIGNED NOT NULL DEFAULT 0,
    ALGORITHM = INSTANT;
