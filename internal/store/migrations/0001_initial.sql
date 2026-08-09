-- +goose Up
CREATE TABLE searches (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL,
    query           TEXT NOT NULL,
    check_interval_s INTEGER NOT NULL,
    next_check_at   INTEGER NOT NULL,
    created_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE TABLE products (
    id              INTEGER PRIMARY KEY,
    search_id       INTEGER NOT NULL REFERENCES searches(id) ON DELETE CASCADE,
    url             TEXT NOT NULL,
    last_checked_at INTEGER,
    created_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE (search_id, url)
);

CREATE INDEX idx_products_search_id ON products(search_id);

-- +goose Down
DROP TABLE products;
DROP TABLE searches;
