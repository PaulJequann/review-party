package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	ctx, stop := signalContext(context.Background())
	defer stop()
	os.Exit(execute(ctx, os.Args[1:], productionCommandIO(os.Stdin, os.Stdout, os.Stderr)))
}

// signalContext cancels on the first termination signal and then releases
// the handlers, so a second signal terminates the process with the default
// disposition while cleanup is still running. A signal the process started
// with ignored, such as SIGHUP under nohup, stays ignored: naming it would
// turn it back on.
func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	var signals []os.Signal
	for _, termination := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(termination) {
			signals = append(signals, termination)
		}
	}
	if len(signals) == 0 {
		// NotifyContext with no signals would relay every signal.
		return context.WithCancel(parent)
	}
	ctx, stop := signal.NotifyContext(parent, signals...)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx, stop
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return execute(ctx, arguments, productionCommandIO(strings.NewReader(""), stdout, stderr))
}

func productionCommandIO(input io.Reader, output, errorOutput io.Writer) commandIO {
	return commandIO{input: input, output: output, errors: errorOutput}
}
