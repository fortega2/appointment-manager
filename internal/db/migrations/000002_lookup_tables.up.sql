CREATE TABLE IF NOT EXISTS public.health_insurance (
    id SMALLINT PRIMARY KEY,
    name varchar(50) NOT NULL
);

COMMENT ON TABLE public.health_insurance IS 'Lookup table for health insurance providers';
COMMENT ON COLUMN public.health_insurance.name IS 'Name of the health insurance provider';

INSERT INTO public.health_insurance (id, name)
VALUES
    (1, 'OSDE'),
    (2, 'SWISS MEDICAL'),
    (3, 'GALENO'),
    (4, 'MEDICUS'),
    (5, 'SANCOR SALUD'),
    (6, 'FEMEBA'),
    (7, 'OMINT'),
    (8, 'PREVENCION SALUD'),
    (9, 'MEDIFE'),
    (10, 'OSPAT');

CREATE TABLE IF NOT EXISTS public.appointment_status (
    id SMALLINT PRIMARY KEY,
    name varchar(50) NOT NULL
);

COMMENT ON TABLE public.appointment_status IS 'Lookup table for appointment statuses';
COMMENT ON COLUMN public.appointment_status.name IS 'Name of the appointment status';

INSERT INTO public.appointment_status (id, name)
VALUES
    (1, 'CONFIRMED'),
    (2, 'CANCELLED'),
    (3, 'ABSENT'),
    (4, 'ATTENDED');
