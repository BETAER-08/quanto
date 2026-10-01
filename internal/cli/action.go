package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/BETAER-08/quanto/internal/action"
)

func runAction(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprint(stderr, "quanto: action takes no arguments\n"+usageText)
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return action.Run(ctx, os.Getenv, stdout, stderr, Version)
}
