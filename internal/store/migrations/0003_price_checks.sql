-- +goose Up
-- price_checks stores every price-check attempt: successful (with cents+currency)
-- or failed (with error). Rows are append-only; deal detection reads the last
-- N successful rows per product. Cascade ensures product deletion cleans up.
CREATE TABLE price_checks (
    id          INTEGER PRIMARY KEY,
    product_id  INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    price_cents INTEGER NOT NULL,
    currency    TEXT NOT NULL,
    success     INTEGER NOT NULL,
    error       TEXT,
    checked_at  INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_price_checks_product_id ON price_checks(product_id);
-- Composite: deal detection reads "last N successful checks per product".
CREATE INDEX idx_price_checks_product_checked_at ON price_checks(product_id, checked_at DESC);

-- +goose Down
DROP TABLE price_checks;
