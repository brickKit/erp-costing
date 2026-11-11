-- FIFO: which layers an issue or a purchase return took its units from, how many and at what value (IS01, RT01).
CREATE TABLE issue_layer (
    movement_id       uuid NOT NULL,
    layer_movement_id uuid NOT NULL REFERENCES cost_layer (movement_id),
    quantity          numeric NOT NULL,
    value             numeric NOT NULL,
    PRIMARY KEY (movement_id, layer_movement_id)
);
CREATE INDEX cost_layer_in_stock ON cost_layer (scope_id, ordinal) WHERE issued_quantity < quantity;
