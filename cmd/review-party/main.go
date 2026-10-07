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
// disposition while cleanup is still running.
func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
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
