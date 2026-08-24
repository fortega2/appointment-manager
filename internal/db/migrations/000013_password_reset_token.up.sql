CREATE TABLE IF NOT EXISTS public.password_reset_token (
    id CHAR(64) PRIMARY KEY,
    assistant_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT fk_password_reset_token_assistant
        FOREIGN KEY (assistant_id)
        REFERENCES public.assistant (id)
        ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_password_reset_token_assistant
    ON public.password_reset_token (assistant_id);

COMMENT ON TABLE public.password_reset_token IS 'Outstanding password reset links. A row is a bearer credential, so it is deleted the moment it is redeemed rather than marked used.';
COMMENT ON COLUMN public.password_reset_token.id IS 'Hex-encoded SHA-256 of the token carried by the reset link. The token itself is never stored, so a leaked dump cannot be redeemed.';
COMMENT ON COLUMN public.password_reset_token.created_at IS 'Written by the application rather than defaulted, so it shares a clock with expires_at and with the expiry comparison.';
COMMENT ON COLUMN public.password_reset_token.expires_at IS 'Absolute expiry, written once when the link is issued. Never extended.';
COMMENT ON INDEX public.idx_password_reset_token_assistant IS 'Unique so that at most one link per assistant is ever live: issuing one upserts on this key, which two concurrent requests cannot both slip past the way a delete-then-insert could.';
