UPDATE market_history
SET yes_percent = 100 - yes_percent,
    no_percent = 100 - no_percent
WHERE source = 'chain';
-- migration:split
UPDATE gold_price_history
SET yes_price = 100 - yes_price,
    no_price = 100 - no_price;
