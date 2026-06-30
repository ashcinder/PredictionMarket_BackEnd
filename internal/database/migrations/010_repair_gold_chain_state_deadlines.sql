UPDATE gold_chain_states
SET deadline_sec = FLOOR(deadline_sec / 1000)
WHERE deadline_sec > 10000000000;
-- migration:split
UPDATE gold_chain_states cs
JOIN gold_games g
  ON g.game_id = cs.game_id
 AND LOWER(g.contract_address) = LOWER(cs.contract_address)
SET cs.deadline_sec = g.deadline_sec
WHERE cs.deadline_sec <= 0
  AND g.deadline_sec > 0;
