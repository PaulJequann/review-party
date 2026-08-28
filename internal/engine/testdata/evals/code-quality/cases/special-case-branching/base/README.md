The dispatch package owns decoding and handler lookup. Event-specific policy
belongs in a Handler registered by event kind. New event kinds must not require
changes to the shared dispatch flow.
