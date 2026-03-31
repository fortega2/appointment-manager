CREATE TABLE IF NOT EXISTS assistant (
    id UUID PRIMARY KEY,
    first_name VARCHAR(255) NOT NULL,
    last_name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NULL
);

ALTER TABLE assistant
ADD CONSTRAINT email_must_contain_at_sign
CHECK (position('@' in email) > 0);

ALTER TABLE assistant
ADD CONSTRAINT email_lowercase
CHECK (email = lower(email));
