package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ConsignItem is one listing of the consignment shop (table consign_item).
type ConsignItem struct {
	ID         int64    `json:"id"`
	SellerID   int64    `json:"seller"`
	Tab        int      `json:"tab"`
	TemplateID int      `json:"tid"`
	Quantity   int      `json:"qty"`
	Options    [][2]int `json:"options"`
	PriceBars  int      `json:"price_bars"`
	PriceGem   int      `json:"price_gem"`
	UpTop      bool     `json:"up_top"`
	Sold       bool     `json:"sold"`
}

const consignCols = `id, seller_id, tab, template_id, quantity, options, price_bars, price_gem, up_top, buyer_id IS NOT NULL`

func scanConsign(r pgx.Row) (ConsignItem, error) {
	var c ConsignItem
	var opts []byte
	err := r.Scan(&c.ID, &c.SellerID, &c.Tab, &c.TemplateID, &c.Quantity, &opts, &c.PriceBars, &c.PriceGem, &c.UpTop, &c.Sold)
	if err == nil {
		json.Unmarshal(opts, &c.Options)
	}
	return c, err
}

func consignOne(r pgx.Row) (*ConsignItem, error) {
	c, err := scanConsign(r)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ConsignInsert(ctx context.Context, c *ConsignItem) error {
	opts, _ := json.Marshal(c.Options)
	if c.Options == nil {
		opts = []byte("[]")
	}
	return s.pool.QueryRow(ctx, `INSERT INTO consign_item(seller_id, tab, template_id, quantity, options, price_bars, price_gem)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, c.SellerID, c.Tab, c.TemplateID, c.Quantity, string(opts), c.PriceBars, c.PriceGem).Scan(&c.ID)
}

// ConsignList returns unsold listings of a tab not owned by exclude, up-top first.
func (s *Store) ConsignList(ctx context.Context, tab int, exclude int64, offset, limit int) ([]ConsignItem, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM consign_item WHERE tab=$1 AND buyer_id IS NULL AND seller_id<>$2`,
		tab, exclude).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT `+consignCols+` FROM consign_item WHERE tab=$1 AND buyer_id IS NULL AND seller_id<>$2
		ORDER BY up_top DESC, id DESC OFFSET $3 LIMIT $4`, tab, exclude, offset, limit)
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ConsignItem, error) { return scanConsign(r) })
	return list, total, err
}

func (s *Store) ConsignMine(ctx context.Context, seller int64) ([]ConsignItem, error) {
	rows, _ := s.pool.Query(ctx, `SELECT `+consignCols+` FROM consign_item WHERE seller_id=$1 ORDER BY id`, seller)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ConsignItem, error) { return scanConsign(r) })
}

// ConsignBuy atomically marks an unsold listing as bought if the price still
// matches what the buyer paid. ErrNotFound if it is gone, sold or repriced.
func (s *Store) ConsignBuy(ctx context.Context, id, buyer int64, bars, gem int) (*ConsignItem, error) {
	return consignOne(s.pool.QueryRow(ctx, `UPDATE consign_item SET buyer_id=$2, sold_at=now()
		WHERE id=$1 AND buyer_id IS NULL AND seller_id<>$2 AND price_bars=$3 AND price_gem=$4 RETURNING `+consignCols,
		id, buyer, bars, gem))
}

// ConsignCancel deletes an unsold listing of seller and returns it.
func (s *Store) ConsignCancel(ctx context.Context, id, seller int64) (*ConsignItem, error) {
	return consignOne(s.pool.QueryRow(ctx, `DELETE FROM consign_item WHERE id=$1 AND seller_id=$2 AND buyer_id IS NULL
		RETURNING `+consignCols, id, seller))
}

// ConsignClaim deletes a sold listing of seller and returns it (for the payout).
func (s *Store) ConsignClaim(ctx context.Context, id, seller int64) (*ConsignItem, error) {
	return consignOne(s.pool.QueryRow(ctx, `DELETE FROM consign_item WHERE id=$1 AND seller_id=$2 AND buyer_id IS NOT NULL
		RETURNING `+consignCols, id, seller))
}

func (s *Store) ConsignUpTop(ctx context.Context, id, seller int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE consign_item SET up_top=true WHERE id=$1 AND seller_id=$2 AND buyer_id IS NULL AND NOT up_top`, id, seller)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
