package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/report"
	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/core/source"
)

const devNull = "/dev/null"

type sideInput struct {
	content []byte
	present bool
}

func readSide(path string) (sideInput, error) {
	if path == devNull {
		return sideInput{}, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return sideInput{}, fmt.Errorf("read %s: %w", path, err)
	}
	if len(content) == 0 {
		return sideInput{}, nil
	}
	return sideInput{content: content, present: true}, nil
}

func parseWorkflow(name string, content []byte) (*model.Workflow, []model.Diagnostic, error) {
	doc, err := source.Load(name, content)
	if err != nil {
		return nil, nil, err
	}
	w, diags, err := model.Parse(doc)
	if err != nil {
		return nil, nil, err
	}
	return w, diags, nil
}

func runDiff(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("diff", stderr)
	format := fs.String("format", "text", "output format: text, markdown or json")
	name := fs.String("path", "", "workflow path used in the report")
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return parseExit(err)
	}
	if len(positional) != 2 {
		fmt.Fprint(stderr, "quanto: diff requires <before> and <after>\n"+usageText)
		return exitUsage
	}
	if err := checkFormat(*format, "text", "markdown", "json"); err != nil {
		fmt.Fprintf(stderr, "quanto: %v\n%s", err, usageText)
		return exitUsage
	}
	beforePath, afterPath := positional[0], positional[1]
	before, err := readSide(beforePath)
	if err != nil {
		fmt.Fprintf(stderr, "quanto: %v\n", err)
		return exitError
	}
	after, err := readSide(afterPath)
	if err != nil {
		fmt.Fprintf(stderr, "quanto: %v\n", err)
		return exitError
	}
	display := *name
	if display == "" {
		display = afterPath
		if !after.present {
			display = beforePath
		}
	}
	in := semdiff.Input{Path: display}
	if before.present {
		in.Before, _, in.BeforeErr = parseWorkflow(display, before.content)
	}
	if after.present {
		in.After, _, in.AfterErr = parseWorkflow(display, after.content)
	}
	diffs := []*semdiff.FileDiff{semdiff.Compare(in, semdiff.Options{})}
	switch *format {
	case "markdown":
		return write(stdout, stderr, report.Markdown(diffs, report.Meta{}))
	case "json":
		out, err := report.JSON(diffs, report.Meta{})
		if err != nil {
			fmt.Fprintf(stderr, "quanto: %v\n", err)
			return exitError
		}
		return write(stdout, stderr, string(out))
	}
	return write(stdout, stderr, report.Text(diffs))
}
