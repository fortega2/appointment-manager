ALTER TABLE public.appointment
    ADD COLUMN prescription_id UUID NOT NULL,
    ADD CONSTRAINT fk_appointment_prescription
        FOREIGN KEY (prescription_id)
        REFERENCES public.prescription (id);

COMMENT ON COLUMN public.appointment.prescription_id IS 'References the active prescription that this appointment consumes a session from.';

CREATE INDEX IF NOT EXISTS idx_appointment_prescription
    ON public.appointment (prescription_id);

COMMENT ON INDEX idx_appointment_prescription IS 'Index to optimize counting consumed sessions per prescription.';

CREATE OR REPLACE VIEW public.patient_session_balance AS
SELECT
    p.id AS patient_id,
    pr.id AS prescription_id,
    pr.total_sessions,
    pr.total_sessions - COALESCE(a.consumed_sessions, 0) AS remaining_sessions
FROM
    public.patient p
JOIN
    public.prescription pr ON p.id = pr.patient_id AND pr.status = 1
LEFT JOIN (
    SELECT
        prescription_id,
        COUNT(*) AS consumed_sessions
    FROM
        public.appointment
    WHERE
        status IN (1, 3, 4)
    GROUP BY
        prescription_id
) a ON pr.id = a.prescription_id;

COMMENT ON VIEW public.patient_session_balance IS 'Provides real-time session balance per patient based on their active prescription. Consumed sessions count appointments with status CONFIRMED (1), ABSENT (3), or ATTENDED (4); CANCELLED (2) appointments free the session.';
