DELETE older
FROM gold_price_history AS older
JOIN gold_price_history AS newer
  ON newer.game_id = older.game_id
 AND newer.timestamp_sec = older.timestamp_sec
 AND newer.id > older.id;
-- migration:split
ALTER TABLE gold_price_history
  ADD UNIQUE INDEX uq_gold_price_history_game_time (game_id, timestamp_sec);
