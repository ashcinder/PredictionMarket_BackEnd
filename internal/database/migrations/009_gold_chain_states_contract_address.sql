ALTER TABLE gold_chain_states ADD COLUMN contract_address VARCHAR(42) NOT NULL DEFAULT '' AFTER game_id;
-- migration:split
UPDATE gold_chain_states cs
LEFT JOIN gold_games g ON g.game_id = cs.game_id
SET cs.contract_address = LOWER(COALESCE(NULLIF(g.contract_address, ''), cs.contract_address))
WHERE cs.contract_address = '';
-- migration:split
ALTER TABLE gold_chain_states DROP PRIMARY KEY;
-- migration:split
ALTER TABLE gold_chain_states ADD PRIMARY KEY (contract_address, game_id);
-- migration:split
ALTER TABLE gold_chain_states ADD INDEX idx_gold_chain_states_game (game_id);
