-- erp/costing's tables (docs/design.md, "拥有的数据"). The runtime role reads and writes them; the migration creates
-- no role and grants nothing (protocol 19).

-- Every cost fact this component published, by its ID: what FactService.Read answers (protocol 05, common rules: a
-- consumer that missed one reads it back).
CREATE TABLE costing_fact (
    fact_id     uuid NOT NULL,
    type        text NOT NULL,
    data        jsonb NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (fact_id, type)
);
