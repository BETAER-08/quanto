package store

import (
	"context"
	"fmt"
	"time"
)

func (s *Store) DeliverySeen(ctx context.Context, id string) (bool, error) {
	var seen bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM webhook_deliveries WHERE delivery_id = $1)", id).Scan(&seen); err != nil {
		return false, fmt.Errorf("store: check delivery: %w", err)
	}
	return seen, nil
}

func (s *Store) RecordDelivery(ctx context.Context, id, event string) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO webhook_deliveries (delivery_id, event) VALUES ($1, $2)
ON CONFLICT DO NOTHING`, id, event); err != nil {
		return fmt.Errorf("store: record delivery: %w", err)
	}
	return nil
}

func (s *Store) PruneDeliveries(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, "DELETE FROM webhook_deliveries WHERE received_at < now() - make_interval(secs => $1)", olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("store: prune deliveries: %w", err)
	}
	return tag.RowsAffected(), nil
}
