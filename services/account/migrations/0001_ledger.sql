-- +goose Up
-- Double-entry ledger (app spec §5.1). Money is BIGINT VND, never floating point.

CREATE TABLE accounts (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    number       CHAR(10)    NOT NULL UNIQUE,
    type         TEXT        NOT NULL CHECK (type IN ('CUSTOMER', 'MERCHANT', 'SYSTEM')),
    owner_sub    TEXT        UNIQUE,
    display_name TEXT        NOT NULL,
    category     TEXT        CHECK (category IN ('utilities', 'food', 'shopping', 'transport', 'entertainment')),
    status       TEXT        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'CLOSED')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- only customers have a Cognito owner; only merchants have a category
    CHECK ((type = 'CUSTOMER') = (owner_sub IS NOT NULL)),
    CHECK ((type = 'MERCHANT') = (category IS NOT NULL))
);

-- separate from accounts so a transfer locks the narrowest row possible
CREATE TABLE balances (
    account_id     BIGINT      PRIMARY KEY REFERENCES accounts (id),
    balance        BIGINT      NOT NULL DEFAULT 0,
    allow_negative BOOLEAN     NOT NULL DEFAULT false,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- a CHECK only sees its own row, hence the allow_negative column
    CHECK (allow_negative OR balance >= 0)
);

CREATE TABLE transactions (
    id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type                 TEXT        NOT NULL CHECK (type IN ('OPENING', 'TRANSFER', 'PAYMENT')),
    initiator_account_id BIGINT      NOT NULL REFERENCES accounts (id),
    idempotency_key      TEXT        NOT NULL,
    request_hash         TEXT        NOT NULL,
    amount               BIGINT      NOT NULL CHECK (amount > 0),
    category             TEXT,
    note                 TEXT        CHECK (char_length(note) <= 140),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- the same request sent twice must not move money twice
    UNIQUE (initiator_account_id, idempotency_key)
);

-- one row per account touched; the amounts of a transaction sum to 0 (checked in code, PR 3)
CREATE TABLE postings (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transaction_id BIGINT NOT NULL REFERENCES transactions (id),
    account_id     BIGINT NOT NULL REFERENCES accounts (id),
    amount         BIGINT NOT NULL CHECK (amount <> 0),
    balance_after  BIGINT NOT NULL
);
CREATE INDEX postings_transaction_idx ON postings (transaction_id);
-- history: newest first per account
CREATE INDEX postings_account_idx ON postings (account_id, id DESC);

-- replay protection for transaction tokens
CREATE TABLE used_jti (
    jti        TEXT        PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX used_jti_expires_idx ON used_jti (expires_at);

-- events waiting for the relay to publish to SNS (app spec §5.3)
CREATE TABLE outbox (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id      UUID        NOT NULL UNIQUE,
    type          TEXT        NOT NULL,
    payload       JSONB       NOT NULL,
    trace_context TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ
);
CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE outbox;
DROP TABLE used_jti;
DROP TABLE postings;
DROP TABLE transactions;
DROP TABLE balances;
DROP TABLE accounts;
