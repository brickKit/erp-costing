-- The price sources of the layers (docs/design.md LY02-LY05, AD01, AD02).

-- A basis is usable once converted to the legal entity's base currency (LY02): its base price and the rate taken.
ALTER TABLE basis ADD COLUMN base_price numeric, ADD COLUMN rate jsonb;

-- erp/purchase's withdrawals of bases, by receipt line; one before its basis is kept too (LY05).
CREATE TABLE basis_withdrawal (
    receipt_line_id uuid PRIMARY KEY,
    fact_id         uuid NOT NULL,
    data            jsonb NOT NULL,
    received_at     timestamptz NOT NULL DEFAULT now()
);

-- An invoice's source on a layer covers the units from start on (LY03): a confirmation may cover several stretches.
ALTER TABLE layer_source ADD COLUMN start numeric;

-- erp/finance's price confirmations (LY03), in the order this component received them: the unit price converted to
-- the base currency (null until then), the supplier, the date; reversed by a reversal (LY04).
CREATE TABLE confirmation (
    fact_id         uuid PRIMARY KEY,
    legal_entity_id uuid NOT NULL,
    supplier_id     uuid,
    unit_price      numeric NOT NULL,
    currency        text NOT NULL,
    business_date   date NOT NULL,
    base_price      numeric,
    rate            jsonb,
    arrival         bigint GENERATED ALWAYS AS IDENTITY,
    reversed        boolean NOT NULL DEFAULT false,
    data            jsonb NOT NULL,
    received_at     timestamptz NOT NULL DEFAULT now()
);

-- A confirmation's matches: a receipt movement and its quantity; pending until the layer takes it.
CREATE TABLE confirmation_match (
    confirmation_id uuid NOT NULL REFERENCES confirmation (fact_id),
    movement_id     uuid NOT NULL,
    quantity        numeric NOT NULL,
    state           text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'applied', 'void')),
    PRIMARY KEY (confirmation_id, movement_id)
);
CREATE INDEX confirmation_match_movement ON confirmation_match (movement_id) WHERE state = 'pending';

-- erp/finance's reversals of confirmations, by the confirmation; one before its confirmation is kept too (LY04).
CREATE TABLE reversal (
    confirmed_fact_id uuid PRIMARY KEY,
    fact_id           uuid NOT NULL,
    business_date     date NOT NULL,
    data              jsonb NOT NULL,
    received_at       timestamptz NOT NULL DEFAULT now()
);
