package compaction

import (
	agenthistory "github.com/alfredxw/denova/agent/context/history"
)

const CompactionCapability = agenthistory.CompactionCapability

type CompactionAction = agenthistory.CompactionAction

const CompactionNone = agenthistory.CompactionNone
const CompactionCreate = agenthistory.CompactionCreate

type CompactionState = agenthistory.CompactionState
type CompactionGroup = agenthistory.CompactionGroup
type CompactionPlan = agenthistory.CompactionPlan
type CompactionValidationPolicy = agenthistory.CompactionValidationPolicy
type CompactionMetrics = agenthistory.CompactionMetrics
