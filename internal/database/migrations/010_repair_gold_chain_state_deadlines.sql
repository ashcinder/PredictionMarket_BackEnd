UPDATE gold_chain_states cs
JOIN gold_games g
  ON g.game_id = cs.game_id
 AND LOWER(g.contract_address) = LOWER(cs.contract_address)
SET cs.deadline_sec = g.deadline_sec
WHERE g.deadline_sec > 0
  AND (
    cs.deadline_sec <= 0
    OR cs.deadline_sec > 10000000000
    OR ABS(cs.deadline_sec - g.deadline_sec) > 3600
  );
