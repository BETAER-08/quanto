package report

import (
	"encoding/json"
	"fmt"

	"github.com/BETAER-08/quanto/core/semdiff"
)

const schemaVersion = "quanto.diff/v1"

type document struct {
	Schema       string              `json:"schema"`
	HeadSHA      string              `json:"head_sha"`
	SkippedFiles int                 `json:"skipped_files"`
	Files        []*semdiff.FileDiff `json:"files"`
}

func JSON(diffs []*semdiff.FileDiff, meta Meta) ([]byte, error) {
	doc := document{
		Schema:       schemaVersion,
		HeadSHA:      meta.HeadSHA,
		SkippedFiles: meta.SkippedFiles,
		Files:        sortedDiffs(diffs),
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal report: %w", err)
	}
	return append(out, '\n'), nil
}
