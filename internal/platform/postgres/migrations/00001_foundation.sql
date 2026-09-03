-- +goose Up
-- Establishes the initial migration version without introducing domain tables.
SELECT 1;

-- +goose Down
SELECT 1;
