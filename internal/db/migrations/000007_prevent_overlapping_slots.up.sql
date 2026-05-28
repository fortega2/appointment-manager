CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE public.slot
    ADD CONSTRAINT chk_no_overlapping_slots
    EXCLUDE USING gist (
        professional_id WITH =,
        tstzrange(start_time, end_time) WITH &&
    );

COMMENT ON CONSTRAINT chk_no_overlapping_slots ON public.slot IS 'Ensures a professional cannot have overlapping time slots.';
