-- What the user surface lists by (docs/design.md, 命令与查询).

-- A cost fact's legal entity, product, movement, rule, date and the input that made it (CF01), for the fact list.
ALTER TABLE costing_fact ADD COLUMN legal_entity_id uuid, ADD COLUMN product_id uuid, ADD COLUMN movement_id uuid,
    ADD COLUMN rule_key text, ADD COLUMN business_date date, ADD COLUMN trigger_kind text, ADD COLUMN trigger_id uuid;
CREATE INDEX costing_fact_entity ON costing_fact (legal_entity_id, recorded_at);
CREATE INDEX costing_fact_movement ON costing_fact (movement_id);

-- A layer with units still in stock (the layer list's filter).
ALTER TABLE cost_layer ADD COLUMN in_stock boolean GENERATED ALWAYS AS (issued_quantity < quantity) STORED;
