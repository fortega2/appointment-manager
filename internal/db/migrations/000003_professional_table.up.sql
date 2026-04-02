CREATE TABLE IF NOT EXISTS public.professional (
    id UUID PRIMARY KEY,
    first_name VARCHAR(255) NOT NULL,
    last_name VARCHAR(255) NOT NULL,
    phone TEXT NOT NULL,
    specialty VARCHAR(50) NOT NULL DEFAULT 'kinesiology',
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NULL,
    CONSTRAINT chk_professional_specialty CHECK (specialty = 'kinesiology')
);

COMMENT ON TABLE public.professional IS 'Table to store professionals (e.g., kinesiologist) who provide services to patients.';
COMMENT ON COLUMN public.professional.specialty IS 'Default specialty is kinesiology';
