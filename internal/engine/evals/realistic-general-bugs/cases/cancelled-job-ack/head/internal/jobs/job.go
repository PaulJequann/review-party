package jobs

import "context"

type Job interface {
	Ack(context.Context) error
	Retry(context.Context) error
}

type Handler interface {
	Handle(context.Context) error
}

func Consume(ctx context.Context, job Job, handler Handler) error {
	if err := handler.Handle(ctx); err != nil {
		return job.Retry(context.WithoutCancel(ctx))
	}
	return job.Ack(context.WithoutCancel(ctx))
}
