package jobs

type Job func() error

type Options struct {
	ContinueOnError bool
	DryRun          bool
}

type Runner struct{}

func (Runner) Run(job Job, options Options) error {
	if options.DryRun {
		if options.ContinueOnError {
			return nil
		}
		return nil
	}
	if err := job(); err != nil && !options.ContinueOnError {
		return err
	}
	return nil
}
