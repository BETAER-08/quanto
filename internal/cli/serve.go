package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/BETAER-08/quanto/internal/app"
	"github.com/BETAER-08/quanto/internal/config"
	"github.com/BETAER-08/quanto/internal/store"
)

func newLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("serve", stderr)
	roleFlag := fs.String("role", "", "process role: web, worker or all")
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return parseExit(err)
	}
	if len(positional) != 0 {
		fmt.Fprint(stderr, "quanto: serve takes no positional arguments\n"+usageText)
		return exitUsage
	}
	role, ok := app.ParseRole(*roleFlag)
	if !ok {
		fmt.Fprintf(stderr, "quanto: --role must be web, worker or all\n%s", usageText)
		return exitUsage
	}
	cfg, err := config.FromEnv(config.PurposeServe)
	if err != nil {
		fmt.Fprintf(stderr, "quanto: %v\n", err)
		return exitError
	}
	logger := newLogger(stderr, cfg.LogLevel)
	logger.Info("starting", "version", Version, "role", string(role), "config", cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Serve(ctx, cfg, role, Version, logger); err != nil {
		logger.Error("serve failed", "error", err.Error())
		return exitError
	}
	logger.Info("stopped")
	return exitOK
}

func runMigrate(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("migrate", stderr)
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return parseExit(err)
	}
	if len(positional) != 0 {
		fmt.Fprint(stderr, "quanto: migrate takes no arguments\n"+usageText)
		return exitUsage
	}
	cfg, err := config.FromEnv(config.PurposeMigrate)
	if err != nil {
		fmt.Fprintf(stderr, "quanto: %v\n", err)
		return exitError
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	st, err := store.Open(ctx, cfg.DatabaseURL, cfg.PoolSize())
	if err != nil {
		fmt.Fprintf(stderr, "quanto: %v\n", err)
		return exitError
	}
	defer st.Close()
	applied, err := st.Migrate(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "quanto: %v\n", err)
		return exitError
	}
	if len(applied) == 0 {
		return write(stdout, stderr, "no migrations to apply\n")
	}
	out := ""
	for _, v := range applied {
		out += "applied " + v + "\n"
	}
	return write(stdout, stderr, out)
}
