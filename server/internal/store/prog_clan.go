package store

import "context"

// ProgAddClanPoints adds clan points (legacy capsuleClan) earned by clan tasks.
func (s *Store) ProgAddClanPoints(ctx context.Context, clan, n int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE clan SET points = points + $2 WHERE id = $1`, clan, n)
	return err
}
