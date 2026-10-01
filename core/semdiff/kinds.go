package semdiff

const (
	kindWorkflowAdded          = "workflow.added"
	kindWorkflowRemoved        = "workflow.removed"
	kindWorkflowRenamed        = "workflow.renamed"
	kindWorkflowUnanalyzable   = "workflow.unanalyzable"
	kindTriggerAdded           = "trigger.added"
	kindTriggerRemoved         = "trigger.removed"
	kindTriggerFilterChanged   = "trigger.filter_changed"
	kindTriggerScheduleChanged = "trigger.schedule_changed"
	kindTriggerPRTargetAdded   = "trigger.pull_request_target_added"
	kindJobAdded               = "job.added"
	kindJobRemoved             = "job.removed"
	kindJobRenamed             = "job.renamed"
	kindJobRunnerChanged       = "job.runner_changed"
	kindJobTimeoutChanged      = "job.timeout_changed"
	kindJobConcurrencyChanged  = "job.concurrency_changed"
	kindMatrixCountChanged     = "matrix.count_changed"
	kindMatrixDynamic          = "matrix.dynamic"
	kindMatrixOverLimit        = "matrix.over_limit"
	kindGraphDepthChanged      = "graph.depth_changed"
	kindGraphWidthChanged      = "graph.width_changed"
	kindGraphCycle             = "graph.cycle"
	kindGraphUnresolved        = "graph.unresolved"
	kindPermissionsBroadened   = "permissions.broadened"
	kindPermissionsNarrowed    = "permissions.narrowed"
	kindPermissionsWriteAll    = "permissions.write_all"
	kindPermissionsRemoved     = "permissions.removed"
	kindPermissionsDeclared    = "permissions.declared"
	kindSecretsAdded           = "secrets.added"
	kindSecretsInheritAdded    = "secrets.inherit_added"
	kindActionAdded            = "action.added"
	kindActionRemoved          = "action.removed"
	kindActionThirdPartyAdded  = "action.third_party_added"
	kindActionRefChanged       = "action.ref_changed"
	kindActionPinRemoved       = "action.pin_removed"
	kindEstimateChanged        = "estimate.changed"
)

type kindSpec struct {
	name         string
	significance Significance
}

var kindTable = []kindSpec{
	{kindWorkflowAdded, Normal},
	{kindWorkflowRemoved, Normal},
	{kindWorkflowRenamed, Low},
	{kindWorkflowUnanalyzable, Normal},
	{kindTriggerAdded, Normal},
	{kindTriggerRemoved, Normal},
	{kindTriggerFilterChanged, Normal},
	{kindTriggerScheduleChanged, Normal},
	{kindTriggerPRTargetAdded, High},
	{kindJobAdded, Normal},
	{kindJobRemoved, Normal},
	{kindJobRenamed, Low},
	{kindJobRunnerChanged, Normal},
	{kindJobTimeoutChanged, Low},
	{kindJobConcurrencyChanged, Low},
	{kindMatrixCountChanged, Normal},
	{kindMatrixDynamic, Normal},
	{kindMatrixOverLimit, High},
	{kindGraphDepthChanged, Normal},
	{kindGraphWidthChanged, Normal},
	{kindGraphCycle, High},
	{kindGraphUnresolved, Normal},
	{kindPermissionsBroadened, High},
	{kindPermissionsNarrowed, Low},
	{kindPermissionsWriteAll, High},
	{kindPermissionsRemoved, High},
	{kindPermissionsDeclared, Low},
	{kindSecretsAdded, Normal},
	{kindSecretsInheritAdded, High},
	{kindActionAdded, Low},
	{kindActionRemoved, Low},
	{kindActionThirdPartyAdded, High},
	{kindActionRefChanged, Normal},
	{kindActionPinRemoved, High},
	{kindEstimateChanged, Normal},
}

var kindRank = func() map[string]int {
	m := make(map[string]int, len(kindTable))
	for i, k := range kindTable {
		m[k.name] = i
	}
	return m
}()

func Kinds() []string {
	out := make([]string, len(kindTable))
	for i, k := range kindTable {
		out[i] = k.name
	}
	return out
}

func defaultSignificance(kind string) Significance {
	if i, ok := kindRank[kind]; ok {
		return kindTable[i].significance
	}
	return Normal
}

func rankOf(kind string) int {
	if i, ok := kindRank[kind]; ok {
		return i
	}
	return len(kindTable)
}
