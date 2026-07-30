ALTER TABLE gold_trades
  MODIFY COLUMN trade_type VARCHAR(20) NOT NULL;

-- migration:split
ALTER TABLE gold_trades
  DROP CHECK chk_trades_type;

-- migration:split
ALTER TABLE gold_trades
  ADD COLUMN returned_yes_wei VARCHAR(78) NULL AFTER share_amount_wei,
  ADD COLUMN returned_no_wei VARCHAR(78) NULL AFTER returned_yes_wei;
