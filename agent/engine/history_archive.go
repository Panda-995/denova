package engine

import (
	agenthistory "github.com/alfredxw/denova/agent/context/history"
)

func transcriptVersion(archive *agenthistory.HistoryArchive) uint16 {
	if archive != nil {
		return 2
	}
	return engineTranscriptVersion
}
