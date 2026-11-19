-- +goose Up
CREATE INDEX trips_stale_scan_idx ON trips ((COALESCE(last_position_at, started_at)) ASC) WHERE status = 'active';

-- +goose Down
DROP INDEX IF EXISTS trips_stale_scan_idx;
