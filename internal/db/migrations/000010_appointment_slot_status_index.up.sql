CREATE INDEX IF NOT EXISTS idx_appointment_slot_confirmed
    ON public.appointment (slot_id) WHERE status = 1;

COMMENT ON INDEX idx_appointment_slot_confirmed IS 'Partial index to optimize cancelling every confirmed appointment of a slot. The existing idx_appointment_slot_patient_active cannot serve these lookups because its leading column is patient_id.';