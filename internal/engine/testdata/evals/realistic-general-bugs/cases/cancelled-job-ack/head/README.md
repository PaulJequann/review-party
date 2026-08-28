# Media worker

The queue cancels a job context when its lease is lost or the worker is
shutting down. Cancellation is retryable: handlers must return it to the
consumer so the job is not acknowledged before durable output exists.
