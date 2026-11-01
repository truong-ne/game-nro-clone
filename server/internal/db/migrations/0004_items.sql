-- Consignment shop (cửa hàng ký gửi, legacy shop_ky_gui/). One row per listing;
-- buyer_id is set when sold and the row is deleted when the seller cancels or
-- claims the money. Prices: price_bars in thỏi vàng (item 457) or price_gem.
CREATE TABLE IF NOT EXISTS consign_item (
    id          bigserial PRIMARY KEY,
    seller_id   bigint      NOT NULL REFERENCES character(id) ON DELETE CASCADE,
    tab         smallint    NOT NULL,
    template_id integer     NOT NULL REFERENCES item_template(id),
    quantity    integer     NOT NULL CHECK (quantity > 0),
    options     jsonb       NOT NULL DEFAULT '[]',
    price_bars  integer     NOT NULL DEFAULT 0 CHECK (price_bars >= 0),
    price_gem   integer     NOT NULL DEFAULT 0 CHECK (price_gem >= 0),
    up_top      boolean     NOT NULL DEFAULT false,
    buyer_id    bigint,
    sold_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS consign_item_open ON consign_item (tab, up_top DESC, id DESC) WHERE buyer_id IS NULL;
CREATE INDEX IF NOT EXISTS consign_item_seller ON consign_item (seller_id);
