ALTER TABLE ai_decisions DROP CHECK chk_ai_decisions_outcome;
-- migration:split
ALTER TABLE ai_decisions ADD CONSTRAINT chk_ai_decisions_outcome CHECK (outcome IN (
  'pending','history_insufficient','invalid_reserves','hold',
  'low_confidence','cooldown','traded','trade_failed',
  'sync_failed','sync_cooldown','metadata_unavailable','quote_unavailable'
));
