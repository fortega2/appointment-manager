CREATE TABLE IF NOT EXISTS assistant (
    id UUID PRIMARY KEY,
    first_name VARCHAR(255) NOT NULL,
    last_name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NULL,
    CONSTRAINT chk_assistant_email_format CHECK (position('@' in email) > 0),
    CONSTRAINT chk_assistant_email_lowercase CHECK (email = lower(email))
);
