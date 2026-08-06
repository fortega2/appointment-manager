CREATE TABLE IF NOT EXISTS public.session (
    id CHAR(64) PRIMARY KEY,
    assistant_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT fk_session_assistant
        FOREIGN KEY (assistant_id)
        REFERENCES public.assistant (id)
        ON DELETE CASCADE
);

COMMENT ON TABLE public.session IS 'Active assistant sessions, so a process restart does not log everyone out.';
COMMENT ON COLUMN public.session.id IS 'Hex-encoded SHA-256 of the session token carried by the cookie. The token itself is never stored, so a leaked dump cannot be replayed.';
COMMENT ON COLUMN public.session.created_at IS 'Written by the application rather than defaulted, so it shares a clock with expires_at and with the expiry comparison.';
COMMENT ON COLUMN public.session.expires_at IS 'Absolute expiry, written once at login. Never extended.';
