package semdiff

import (
	"fmt"
	"time"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

type Status string

const (
	StatusAdded        Status = "added"
	StatusRemoved      Status = "removed"
	StatusModified     Status = "modified"
	StatusRenamed      Status = "renamed"
	StatusUnanalyzable Status = "unanalyzable"
)

type Significance int

const (
	Low Significance = iota
	Normal
	High
)

func (s Significance) String() string {
	switch s {
	case Low:
		return "low"
	case Normal:
		return "normal"
	case High:
		return "high"
	}
	return fmt.Sprintf("significance(%d)", int(s))
}

func (s Significance) MarshalText() ([]byte, error) {
	switch s {
	case Low, Normal, High:
		return []byte(s.String()), nil
	}
	return nil, fmt.Errorf("unknown significance %d", int(s))
}

type Input struct {
	Path      string          `json:"path"`
	OldPath   string          `json:"old_path"`
	Before    *model.Workflow `json:"-"`
	After     *model.Workflow `json:"-"`
	BeforeErr error           `json:"-"`
	AfterErr  error           `json:"-"`
}

type DurationSource interface {
	JobAverage(workflowPath, jobKey string) (time.Duration, int, bool)
}

type Options struct {
	Durations  DurationSource `json:"-"`
	MinSamples int            `json:"min_samples"`
}

type Finding struct {
	Kind         string          `json:"kind"`
	Significance Significance    `json:"significance"`
	Subject      string          `json:"subject"`
	Before       string          `json:"before"`
	After        string          `json:"after"`
	Detail       string          `json:"detail"`
	Pos          source.Position `json:"pos"`
	BasePos      source.Position `json:"base_pos"`
}

type Metrics struct {
	JobsPerRun    string `json:"jobs_per_run"`
	Depth         int    `json:"depth"`
	Width         string `json:"width"`
	RunnerMinutes string `json:"runner_minutes"`
}

type Estimate struct {
	MinutesBefore float64 `json:"minutes_before"`
	MinutesAfter  float64 `json:"minutes_after"`
	Samples       int     `json:"samples"`
}

type FileDiff struct {
	Path     string    `json:"path"`
	OldPath  string    `json:"old_path"`
	Status   Status    `json:"status"`
	Before   Metrics   `json:"before"`
	After    Metrics   `json:"after"`
	Findings []Finding `json:"findings"`
	Estimate *Estimate `json:"estimate"`
	Error    string    `json:"error"`
}
