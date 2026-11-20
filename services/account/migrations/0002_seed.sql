-- +goose Up
-- System account that funds the 10,000,000 VND opening gift; the only account allowed below 0.
WITH a AS (
    INSERT INTO accounts (number, type, display_name)
    VALUES ('0000000000', 'SYSTEM', 'BANK_FUNDING')
    RETURNING id
)
INSERT INTO balances (account_id, allow_negative) SELECT id, true FROM a;

-- Fake merchants (generic names, no real brands) for payments by category.
WITH m AS (
    INSERT INTO accounts (number, type, display_name, category)
    VALUES
    ('9000000019', 'MERCHANT', 'CONG TY DIEN NUOC', 'utilities'),
    ('9000000027', 'MERCHANT', 'QUAN AN NGON', 'food'),
    ('9000000035', 'MERCHANT', 'CUA HANG TIEN LOI', 'shopping'),
    ('9000000043', 'MERCHANT', 'XE CONG NGHE', 'transport'),
    ('9000000050', 'MERCHANT', 'RAP CHIEU PHIM', 'entertainment')
    RETURNING id
)
INSERT INTO balances (account_id) SELECT id FROM m;

-- +goose Down
DELETE FROM balances WHERE account_id IN (SELECT id FROM accounts WHERE type IN ('SYSTEM', 'MERCHANT'));
DELETE FROM accounts WHERE type IN ('SYSTEM', 'MERCHANT');
