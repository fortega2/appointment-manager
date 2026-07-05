DROP VIEW IF EXISTS public.patient_session_balance;

ALTER TABLE public.appointment
    DROP CONSTRAINT IF EXISTS fk_appointment_prescription;

DROP INDEX IF EXISTS idx_appointment_prescription;

ALTER TABLE public.appointment
    DROP COLUMN IF EXISTS prescription_id;
