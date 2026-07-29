ALTER TABLE ai_decisions DROP CHECK chk_ai_decisions_action;
-- migration:split
ALTER TABLE ai_decisions ADD CONSTRAINT chk_ai_decisions_action CHECK (
  action IN ('buy_yes','buy_no','sell_yes','sell_no','hold')
);
