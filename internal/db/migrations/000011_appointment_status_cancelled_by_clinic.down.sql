-- fk_appointment_status blocks the DELETE while any appointment still points at
-- status 5, so those rows are demoted to CANCELLED (2) first. This is lossy by
-- design: it restores exactly the pre-migration state, where a clinic
-- cancellation was indistinguishable from a patient one.
UPDATE public.appointment
SET status = 2, updated_at = CURRENT_TIMESTAMP
WHERE status = 5;

DELETE FROM public.appointment_status
WHERE id = 5;
