CREATE TABLE IF NOT EXISTS public.prescription_status (
    id SMALLINT PRIMARY KEY,
    name VARCHAR(50) NOT NULL
);

COMMENT ON TABLE public.prescription_status IS 'Lookup table for prescription statuses';
COMMENT ON COLUMN public.prescription_status.name IS 'ACTIVE, COMPLETED, or CANCELLED';

INSERT INTO public.prescription_status (id, name)
VALUES
    (1, 'ACTIVE'),
    (2, 'COMPLETED'),
    (3, 'CANCELLED');

CREATE TABLE IF NOT EXISTS public.prescription (
    id UUID PRIMARY KEY,
    patient_id UUID NOT NULL,
    file_path TEXT NOT NULL,
    total_sessions SMALLINT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_prescription_patient
        FOREIGN KEY (patient_id)
        REFERENCES public.patient (id)
        ON DELETE CASCADE,
    CONSTRAINT fk_prescription_status
        FOREIGN KEY (status)
        REFERENCES public.prescription_status (id),
    CONSTRAINT chk_prescription_sessions CHECK (total_sessions > 0)
);

COMMENT ON TABLE public.prescription IS 'Stores digital prescriptions that authorize a patient for a limited number of sessions. Only one prescription per patient may be ACTIVE at a time.';
COMMENT ON COLUMN public.prescription.patient_id IS 'References the patient who owns this prescription.';
COMMENT ON COLUMN public.prescription.file_path IS 'File path to the digital copy of the prescription document.';
COMMENT ON COLUMN public.prescription.total_sessions IS 'Total number of sessions authorized by this prescription.';
COMMENT ON COLUMN public.prescription.status IS 'Current status of the prescription: 1=ACTIVE, 2=COMPLETED, 3=CANCELLED.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_prescription_active_per_patient
    ON public.prescription (patient_id) WHERE status = 1;

COMMENT ON INDEX idx_prescription_active_per_patient IS 'Ensures at most one active prescription per patient at any time.';
