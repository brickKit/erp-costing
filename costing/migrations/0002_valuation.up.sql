-- The costing policy and the valuation sequence (docs/design.md: PO01, VS01-VS05, LY01, IS01).

-- A legal entity's costing method (PO01). A row of version 0 is the default no one set: valuation writes it so that a
-- change and the first valuation of the legal entity take the same row's lock.
CREATE TABLE policy (
    legal_entity_id uuid PRIMARY KEY,
    method          text NOT NULL DEFAULT 'moving_average' CHECK (method IN ('moving_average', 'fifo')),
    version         bigint NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- A valuation scope (product + legal entity): its progress in the valuation sequence and its stock's quantity and
-- value (VS01-VS05). queued: a processing item is pending (one at a time is enough, VS01).
CREATE TABLE scope (
    id              uuid PRIMARY KEY,
    legal_entity_id uuid NOT NULL,
    product_id      uuid NOT NULL,
    epoch           text NOT NULL,
    next_seq        bigint NOT NULL DEFAULT 1,
    state           text NOT NULL DEFAULT 'running' CHECK (state IN ('running', 'waiting_gap', 'waiting_basis', 'stopped')),
    quantity        numeric NOT NULL DEFAULT 0,
    value           numeric NOT NULL DEFAULT 0,
    currency        text NOT NULL DEFAULT '',
    waiting_since   timestamptz,
    stopped_reason  text,
    queued          boolean NOT NULL DEFAULT false,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (legal_entity_id, product_id)
);
CREATE INDEX scope_state ON scope (state);

-- Movements received and not yet valued (VS01): one per number of an epoch; a movement numbered again in a new
-- epoch is another row (VS04).
CREATE TABLE movement_in (
    scope_id    uuid NOT NULL REFERENCES scope (id),
    epoch       text NOT NULL,
    seq         bigint NOT NULL,
    fact_id     uuid NOT NULL,
    data        jsonb NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (scope_id, epoch, seq),
    UNIQUE (fact_id, epoch)
);

-- Every valued movement, once (VS02): its cost is the receipt's value, or the value an issue or a return took.
CREATE TABLE valued_movement (
    fact_id         uuid PRIMARY KEY,
    scope_id        uuid NOT NULL REFERENCES scope (id),
    legal_entity_id uuid NOT NULL,
    product_id      uuid NOT NULL,
    epoch           text NOT NULL,
    seq             bigint NOT NULL,
    kind            text NOT NULL,
    quantity        numeric NOT NULL,
    business_date   date NOT NULL,
    cost            numeric NOT NULL,
    reference       jsonb,
    valued_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (scope_id, epoch, seq)
);
CREATE INDEX valued_movement_entity ON valued_movement (legal_entity_id);

-- A receipt's cost layer (LY01): its quantity, what left it (issues and purchase returns), its weighted unit cost and
-- its highest source. ordinal: the receipts' order in the scope (FIFO; the latest layer's price is the policy price).
CREATE TABLE cost_layer (
    movement_id     uuid PRIMARY KEY, -- the receipt's valued_movement, written in the same transaction
    scope_id        uuid NOT NULL REFERENCES scope (id),
    legal_entity_id uuid NOT NULL,
    product_id      uuid NOT NULL,
    ordinal         bigint GENERATED ALWAYS AS IDENTITY,
    receipt_line_id uuid,
    business_date   date NOT NULL,
    quantity        numeric NOT NULL,
    issued_quantity numeric NOT NULL DEFAULT 0,
    unit_cost       numeric NOT NULL,
    source          text NOT NULL CHECK (source IN ('policy', 'basis', 'invoice')),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX cost_layer_scope ON cost_layer (scope_id, ordinal);
CREATE INDEX cost_layer_line ON cost_layer (receipt_line_id);

-- The price sources of a layer's units (LY01-LY04): kind, the fact that gave it, the quantity it covers, its unit
-- price, its arrival; a withdrawn source is a new state, never deleted.
CREATE TABLE layer_source (
    id          uuid PRIMARY KEY,
    movement_id uuid NOT NULL REFERENCES cost_layer (movement_id),
    kind        text NOT NULL CHECK (kind IN ('policy', 'basis', 'invoice')),
    source_id   uuid,
    quantity    numeric NOT NULL,
    unit_price  numeric NOT NULL,
    arrival     bigint GENERATED ALWAYS AS IDENTITY,
    withdrawn   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX layer_source_movement ON layer_source (movement_id, arrival);

-- erp/purchase's valuation bases by receipt line (LY01, LY02, LY05).
CREATE TABLE basis (
    receipt_line_id uuid PRIMARY KEY,
    legal_entity_id uuid NOT NULL,
    product_id      uuid NOT NULL,
    unit_price      numeric NOT NULL,
    currency        text NOT NULL,
    business_date   date NOT NULL,
    withdrawn_at    timestamptz,
    data            jsonb NOT NULL,
    received_at     timestamptz NOT NULL DEFAULT now()
);
