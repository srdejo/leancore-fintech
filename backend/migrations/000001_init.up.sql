-- Montos en centavos COP (BIGINT). El devengo sin liquidar se guarda en
-- unidades de 10^-15 centavos (NUMERIC(38,0)), sin decimales.
CREATE TABLE credit_lines (
    id                       UUID PRIMARY KEY,
    account_number           TEXT        NOT NULL,
    holder                   TEXT        NOT NULL CHECK (holder <> ''),
    limit_cents              BIGINT      NOT NULL CHECK (limit_cents > 0),
    rate_ea_bps              INTEGER     NOT NULL CHECK (rate_ea_bps >= 0),
    daily_rate_e15           BIGINT      NOT NULL CHECK (daily_rate_e15 >= 0),
    capital_cents            BIGINT      NOT NULL CHECK (capital_cents >= 0),
    interest_due_cents       BIGINT      NOT NULL CHECK (interest_due_cents >= 0),
    accrual_units            NUMERIC(38,0) NOT NULL DEFAULT 0 CHECK (accrual_units >= 0),
    last_accrual_date        DATE        NOT NULL,
    anchor_date              DATE        NOT NULL,
    last_entry_date          DATE        NOT NULL,
    last_seq                 INTEGER     NOT NULL,
    creation_idempotency_key UUID        NOT NULL,
    creation_request_hash    TEXT        NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT credit_lines_account_number_uq UNIQUE (account_number),
    CONSTRAINT credit_lines_creation_key_uq UNIQUE (creation_idempotency_key)
);

-- Movimientos append-only: la aplicación solo inserta.
CREATE TABLE entries (
    id                  UUID PRIMARY KEY,
    credit_line_id      UUID        NOT NULL REFERENCES credit_lines (id),
    seq                 INTEGER     NOT NULL,
    type                TEXT        NOT NULL CHECK (type IN ('DESEMBOLSO', 'CONSUMO', 'INTERES', 'PAGO')),
    amount_cents        BIGINT      NOT NULL CHECK (amount_cents > 0),
    entry_date          DATE        NOT NULL,
    description         TEXT        NOT NULL,
    detail              TEXT        NOT NULL DEFAULT '',
    to_interest_cents   BIGINT      CHECK (to_interest_cents >= 0),
    balance_after_cents BIGINT      NOT NULL,
    idempotency_key     UUID,
    request_hash        TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT entries_seq_uq UNIQUE (credit_line_id, seq),
    CONSTRAINT entries_idempotency_key_uq UNIQUE (credit_line_id, idempotency_key)
);

CREATE TABLE payment_allocations (
    payment_entry_id UUID   NOT NULL REFERENCES entries (id),
    use_entry_id     UUID   NOT NULL REFERENCES entries (id),
    to_capital_cents BIGINT NOT NULL CHECK (to_capital_cents > 0),
    PRIMARY KEY (payment_entry_id, use_entry_id)
);

CREATE INDEX payment_allocations_use_idx ON payment_allocations (use_entry_id);
