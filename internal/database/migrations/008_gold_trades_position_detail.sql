ALTER TABLE gold_trades ADD COLUMN share_amount_wei VARCHAR(78) NOT NULL DEFAULT '0' AFTER amount_wei;
-- migration:split
ALTER TABLE gold_trades ADD COLUMN my_shares_yes_after VARCHAR(78) NULL AFTER is_ai_managed;
-- migration:split
ALTER TABLE gold_trades ADD COLUMN my_shares_no_after VARCHAR(78) NULL AFTER my_shares_yes_after;
