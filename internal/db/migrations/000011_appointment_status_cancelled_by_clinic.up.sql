-- Records who called an appointment off, not merely that it was called off.
-- CANCELLED (2) stays patient-initiated; the clinic cancelling a whole slot now
-- writes this status instead, so notifications can address exactly the patients
-- the clinic owes a message without also reaching patients who had cancelled
-- themselves earlier. The name is spaced rather than underscored because the
-- appointments grid renders it through INITCAP().
INSERT INTO public.appointment_status (id, name)
VALUES (5, 'CANCELLED BY CLINIC')
ON CONFLICT (id) DO NOTHING;
