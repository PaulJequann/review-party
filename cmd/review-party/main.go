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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(execute(ctx, os.Args[1:], productionCommandIO(os.Stdin, os.Stdout, os.Stderr)))
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return execute(ctx, arguments, productionCommandIO(strings.NewReader(""), stdout, stderr))
}

func productionCommandIO(input io.Reader, output, errorOutput io.Writer) commandIO {
	return commandIO{input: input, output: output, errors: errorOutput}
}
