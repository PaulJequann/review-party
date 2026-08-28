package jobs

type Job func() error

type Runner struct{}

func (Runner) Run(job Job) error {
	return job()
}
