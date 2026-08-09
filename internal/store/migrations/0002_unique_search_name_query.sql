-- +goose Up
-- Deduplicate existing rows, keeping the lowest id per (name, query).
DELETE FROM searches WHERE id NOT IN (
    SELECT MIN(id) FROM searches GROUP BY name, query
);

-- Recreate the table with a UNIQUE constraint on (name, query).
CREATE TABLE searches_new (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL,
    query           TEXT NOT NULL,
    check_interval_s INTEGER NOT NULL,
    next_check_at   INTEGER NOT NULL,
    created_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE (name, query)
);

INSERT INTO searches_new (id, name, query, check_interval_s, next_check_at, created_at)
SELECT id, name, query, check_interval_s, next_check_at, created_at FROM searches;

DROP TABLE searches;
ALTER TABLE searches_new RENAME TO searches;

-- +goose Down
DROP TABLE searches;
