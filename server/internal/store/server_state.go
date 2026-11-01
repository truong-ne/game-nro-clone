package store

import (
	"context"
	"encoding/json"
)

// LoadServerState returns every saved server-wide state document by key.
func (s *Store) LoadServerState(ctx context.Context) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	rows, _ := s.pool.Query(ctx, `SELECT key, value FROM server_state`)
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SaveServerState(ctx context.Context, key string, value []byte) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO server_state(key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, value)
	return err
}
