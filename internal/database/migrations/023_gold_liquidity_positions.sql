ALTER TABLE gold_chain_states
    ADD COLUMN total_liquidity_shares VARBINARY(80) NULL AFTER reserve_no,
    ADD COLUMN liquidity_fee_pool VARBINARY(80) NULL AFTER total_liquidity_shares;

-- migration:split

ALTER TABLE gold_user_positions
    ADD COLUMN my_liquidity_shares VARBINARY(80) NULL AFTER my_shares_no,
    ADD COLUMN my_liquidity_fees VARBINARY(80) NULL AFTER my_liquidity_shares;
