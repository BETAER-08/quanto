package model

import (
	"errors"
	"strings"

	"github.com/BETAER-08/quanto/core/expr"
	"github.com/BETAER-08/quanto/core/source"
)

type Diagnostic struct {
	Code    string
	Message string
	Pos     source.Position
}

const (
	codeNoOn                   = "MODEL-NO-ON"
	codeNoJobs                 = "MODEL-NO-JOBS"
	codeJobNotMapping          = "MODEL-JOB-NOT-MAPPING"
	codeStepNotMapping         = "MODEL-STEP-NOT-MAPPING"
	codeStepNoAction           = "MODEL-STEP-NO-ACTION"
	codeActionNoRef            = "MODEL-ACTION-NO-REF"
	codeExprSyntax             = "MODEL-EXPR-SYNTAX"
	codeUnknownPermissionLevel = "MODEL-UNKNOWN-PERMISSION-LEVEL"
)

var ErrNotWorkflow = errors.New("document is not a workflow mapping")

type Workflow struct {
	File        string
	Name        string
	Triggers    []Trigger
	Permissions PermissionSet
	Concurrency *Concurrency
	EnvKeys     []string
	SecretRefs  []string
	Jobs        []*Job
	JobsPos     source.Position
	Pos         source.Position
}

type Trigger struct {
	Event   string
	Filters map[string][]source.Positioned[string]
	Crons   []source.Positioned[string]
	Inputs  []string
	Pos     source.Position
}

type Job struct {
	ID              string
	Name            source.Positioned[string]
	Needs           []source.Positioned[string]
	If              *Condition
	RunsOn          RunnerSpec
	Strategy        *Strategy
	Permissions     PermissionSet
	Environment     string
	Concurrency     *Concurrency
	TimeoutMinutes  *source.Positioned[string]
	ContinueOnError string
	ContainerImage  string
	Services        []string
	Uses            *ReusableRef
	With            map[string]source.Positioned[string]
	SecretsInherit  bool
	SecretNames     []string
	Outputs         []string
	Steps           []*Step
	Pos             source.Position
}

type Step struct {
	Index            int
	ID               string
	Name             string
	If               *Condition
	Uses             *ActionRef
	Run              *source.Positioned[string]
	With             map[string]source.Positioned[string]
	EnvKeys          []string
	Shell            string
	WorkingDirectory string
	Pos              source.Position
}

type Condition struct {
	Raw      string
	Template *expr.Template
	ParseErr error
	Pos      source.Position
}

type RunnerSpec struct {
	Labels  []source.Positioned[string]
	Group   string
	Dynamic bool
	Pos     source.Position
}

type Strategy struct {
	Matrix      *source.Node
	FailFast    string
	MaxParallel string
	Pos         source.Position
}

type Concurrency struct {
	Group            string
	CancelInProgress string
	Pos              source.Position
}

type Level int

const (
	LevelNone Level = iota
	LevelRead
	LevelWrite
)

type PermissionSet struct {
	Declared bool
	All      string
	Scopes   map[string]source.Positioned[Level]
	Pos      source.Position
}

type RefKind int

const (
	RefUnknown RefKind = iota
	RefSHA
	RefMutable
)

type ActionRef struct {
	Raw         string
	Owner       string
	Repo        string
	Path        string
	Ref         string
	Kind        RefKind
	Local       bool
	Docker      bool
	DockerImage string
	FirstParty  bool
	VersionHint string
	Pos         source.Position
}

func (a *ActionRef) Identity() string {
	if a == nil {
		return ""
	}
	if a.Local {
		return a.Path
	}
	if a.Docker {
		return "docker://" + a.DockerImage
	}
	id := strings.ToLower(a.Owner)
	if a.Repo != "" {
		id += "/" + strings.ToLower(a.Repo)
	}
	if a.Path != "" {
		id += "/" + strings.ToLower(a.Path)
	}
	return id
}

type ReusableRef struct {
	Raw   string
	Local bool
	Owner string
	Repo  string
	Path  string
	Ref   string
	Kind  RefKind
	Pos   source.Position
}
