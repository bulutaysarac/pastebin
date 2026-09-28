-- The official MySQL image runs this file on first start only, while the data
-- volume is still empty. Later schema changes will not be picked up this way.

CREATE TABLE pastes (
    -- ascii_bin makes the id case-sensitive. Under MySQL's default collation
    -- (utf8mb4_0900_ai_ci), "aB3xK9pQ" and "AB3XK9PQ" would count as the same key.
    id         CHAR(8)    CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    content    MEDIUMTEXT NOT NULL,  -- up to 16 MB
    language   VARCHAR(32) NOT NULL DEFAULT 'plaintext',  -- syntax highlighting, applied in the browser
    expires_at DATETIME   NULL,      -- UTC; NULL = never expires
    PRIMARY KEY (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
