CREATE TABLE IF NOT EXISTS public.patient (
    id uuid PRIMARY KEY,
    first_name varchar(255) NOT NULL,
    last_name varchar(255) NOT NULL,
    phone text NOT NULL,
    email varchar(255),
    health_insurance smallint NOT NULL,
    insurance_number char(11) NOT NULL,
    clinical_notes text NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NULL,
    CONSTRAINT fk_patient_health_insurance
        FOREIGN KEY (health_insurance) REFERENCES public.health_insurance (id)
);

COMMENT ON TABLE public.patient IS 'Table to store patient information, including personal details, contact information, and health insurance data.';
COMMENT ON COLUMN public.patient.first_name IS 'Patient''s first name';
COMMENT ON COLUMN public.patient.last_name IS 'Patient''s last name';
COMMENT ON COLUMN public.patient.phone IS 'Patient''s phone number';
COMMENT ON COLUMN public.patient.email IS 'Patient''s email address';
COMMENT ON COLUMN public.patient.health_insurance IS 'Foreign key referencing the health insurance provider';
COMMENT ON COLUMN public.patient.insurance_number IS 'Patient''s insurance number, must be exactly 11 characters';
COMMENT ON COLUMN public.patient.clinical_notes IS 'Additional clinical notes about the patient';
COMMENT ON COLUMN public.patient.created_at IS 'Timestamp when the patient record was created';
COMMENT ON COLUMN public.patient.updated_at IS 'Timestamp when the patient record was last updated';
