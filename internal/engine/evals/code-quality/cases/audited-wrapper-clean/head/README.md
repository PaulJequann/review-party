Store writes values. Callers depend on Store and do not know how writes are
persisted. AuditedStore is the canonical boundary for recording durable write
events.
