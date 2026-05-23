CREATE TABLE IF NOT EXISTS public.slot (
    id UUID PRIMARY KEY,
    professional_id UUID NOT NULL,
    date DATE NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    max_capacity SMALLINT NOT NULL,
    blocked BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NULL,
    CONSTRAINT fk_slot_professional
        FOREIGN KEY (professional_id)
        REFERENCES public.professional (id),
    CONSTRAINT chk_slot_times CHECK (end_time > start_time),
    CONSTRAINT chk_slot_capacity CHECK (max_capacity > 0),
    CONSTRAINT chk_slot_date_consistency CHECK (date = start_time::date)
);

COMMENT ON TABLE public.slot IS 'Stores available time slots for professionals. Each slot has a date, time range, capacity limit, and can be blocked if needed.';
COMMENT ON COLUMN public.slot.professional_id IS 'References the professional who owns this slot.';
COMMENT ON COLUMN public.slot.date IS 'The date of the slot.';
COMMENT ON COLUMN public.slot.date IS 'The date of the slot. Denormalized for efficient calendar/week queries. Must match start_time::date.';
COMMENT ON COLUMN public.slot.start_time IS 'The start date and time of the slot with timezone information.';
COMMENT ON COLUMN public.slot.end_time IS 'The end date and time of the slot with timezone information.';
COMMENT ON COLUMN public.slot.max_capacity IS 'The maximum number of patients that can book this slot.';
COMMENT ON COLUMN public.slot.blocked IS 'Indicates whether the slot is blocked and unavailable for booking.';

CREATE INDEX idx_slot_professional_date ON public.slot (professional_id, date);

COMMENT ON INDEX idx_slot_professional_date IS 'Index to optimize queries filtering slots by professional and date.';
