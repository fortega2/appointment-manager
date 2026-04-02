CREATE TABLE IF NOT EXISTS public.appointment (
    id UUID PRIMARY KEY,
    slot_id UUID NOT NULL,
    patient_id UUID NOT NULL,
    professional_id UUID NOT NULL,
    assistant_id UUID NOT NULL,
    status SMALLINT NOT NULL DEFAULT 1,
    notes TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NULL,
    CONSTRAINT fk_appointment_slot
        FOREIGN KEY (slot_id)
        REFERENCES public.slot (id),
    CONSTRAINT fk_appointment_patient
        FOREIGN KEY (patient_id)
        REFERENCES public.patient (id),
    CONSTRAINT fk_appointment_professional
        FOREIGN KEY (professional_id)
        REFERENCES public.professional (id),
    CONSTRAINT fk_appointment_assistant
        FOREIGN KEY (assistant_id)
        REFERENCES public.assistant (id),
    CONSTRAINT fk_appointment_status
        FOREIGN KEY (status)
        REFERENCES public.appointment_status (id)
);

COMMENT ON TABLE public.appointment IS 'Table to store appointments between patients and professionals, including details about the slot, status, and any notes.';
COMMENT ON COLUMN public.appointment.status IS 'Status of the appointment, referencing appointment_status table for possible values.';
COMMENT ON COLUMN public.appointment.notes IS 'Additional notes or comments related to the appointment, such as patient preferences or special instructions.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_appointment_slot_patient_active ON public.appointment (patient_id, slot_id) WHERE status = 1;

COMMENT ON INDEX idx_appointment_slot_patient_active IS 'Unique index to prevent a patient from booking multiple active appointments for the same slot.';
