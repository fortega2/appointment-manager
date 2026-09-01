CREATE TABLE IF NOT EXISTS public.outbox (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    aggregate_type TEXT        NOT NULL,
    aggregate_id   UUID        NOT NULL,
    event_type     TEXT        NOT NULL,
    payload        JSONB       NOT NULL,
    attempts       SMALLINT    NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    available_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at   TIMESTAMPTZ NULL,
    last_error     TEXT        NULL,
    CONSTRAINT chk_outbox_payload_object CHECK (jsonb_typeof(payload) = 'object')
);

COMMENT ON TABLE public.outbox IS 'Transactional outbox: events written in the same transaction as the domain change that produced them, drained later by a worker. Generic rather than notification-specific so any future consumer can share it. Processed rows are kept; the partial index below is what makes that affordable.';

COMMENT ON COLUMN public.outbox.aggregate_id IS 'Intentionally without a foreign key: aggregate_type makes it polymorphic, and the event must outlive the deletion of the row it refers to.';
COMMENT ON COLUMN public.outbox.payload IS 'Identifiers only, never resolved personal data. Recipient details are looked up at send time (ADR 0002 decision 1), so this long-retained table does not become a store of patient PII.';
COMMENT ON COLUMN public.outbox.available_at IS 'Earliest time the row may be picked up, pushed into the future on each failure. That backoff is what stops one failing event from blocking the drain.';
COMMENT ON COLUMN public.outbox.processed_at IS 'NULL means pending, so it is both the flag and the delivery timestamp. Setting it removes the row from idx_outbox_pending.';

CREATE INDEX IF NOT EXISTS idx_outbox_pending
    ON public.outbox (available_at, id)
    WHERE processed_at IS NULL;

COMMENT ON INDEX public.idx_outbox_pending IS 'Holds only the pending backlog, not every event ever produced, which is why processed rows can be retained indefinitely without slowing the drain. Column order is load-bearing: available_at leads because it is the range bound, id breaks ties between rows sharing a transaction timestamp, and a drain ordering by (available_at, id) is then answered without a sort. The query must spell out processed_at IS NULL or the planner cannot use this index at all.';
