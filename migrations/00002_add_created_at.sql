-- +goose Up
-- When the paste was created, in UTC. The app sets it on every insert. The
-- default fills the rows that exist when this runs, with the time it runs, and
-- the inserts of an app version that doesn't know the column yet.
-- INSTANT adds the column without rewriting the table. If MySQL can't do that,
-- the migration fails instead of rebuilding every row while the app waits.

ALTER TABLE pastes
    ADD COLUMN created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ALGORITHM = INSTANT;
