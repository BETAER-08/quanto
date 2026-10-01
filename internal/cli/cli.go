package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

var Version = "dev"

const usageText = `usage:
  quanto version
  quanto inspect <file> [--format text|json]
  quanto diff <before> <after> [--format text|markdown|json] [--path <name>]
  quanto serve --role web|worker|all
  quanto migrate
  quanto manifest --webhook-url <url> --homepage-url <url> [--name quanto]
  quanto action
`

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}
	switch args[0] {
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "inspect":
		return runInspect(args[1:], stdout, stderr)
	case "diff":
		return runDiff(args[1:], stdout, stderr)
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "migrate":
		return runMigrate(args[1:], stdout, stderr)
	case "manifest":
		return runManifest(args[1:], stdout, stderr)
	case "action":
		return runAction(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "quanto: unknown command %q\n%s", args[0], usageText)
	return exitUsage
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprint(stderr, "quanto: version takes no arguments\n"+usageText)
		return exitUsage
	}
	fmt.Fprintln(stdout, Version)
	return exitOK
}

func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usageText)
	}
	return fs
}

func parseExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	return exitUsage
}

func checkFormat(format string, allowed ...string) error {
	for _, a := range allowed {
		if format == a {
			return nil
		}
	}
	return fmt.Errorf("unsupported format %q", format)
}

func write(stdout, stderr io.Writer, data string) int {
	if _, err := io.WriteString(stdout, data); err != nil {
		fmt.Fprintf(stderr, "quanto: write output: %v\n", err)
		return exitError
	}
	return exitOK
}
