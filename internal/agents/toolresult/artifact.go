// Package toolresult owns provider-neutral result normalization and recovery
// metadata shared by execution and model-context maintenance.
package toolresult

import (
	"strings"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// RecoverableArtifactPurpose reports whether an artifact contains complete
// output that can safely replace rich inline model context.
func RecoverableArtifactPurpose(purpose agentschema.ToolArtifactPurpose) bool {
	return purpose == agentschema.ToolArtifactPurposeCompleteModelOutput || purpose == agentschema.ToolArtifactPurposeCompleteToolOutput
}

// CanonicalArtifact normalizes one recovery identity before verification,
// persistence, or context cleanup.
func CanonicalArtifact(artifact agentschema.ToolArtifactRef) agentschema.ToolArtifactRef {
	artifact.Purpose = agentschema.ToolArtifactPurpose(strings.TrimSpace(string(artifact.Purpose)))
	artifact.ReadablePath = strings.TrimSpace(strings.ToValidUTF8(artifact.ReadablePath, "\uFFFD"))
	artifact.ContentType = strings.TrimSpace(artifact.ContentType)
	if artifact.EstimatedTokens == 0 && artifact.EstimatedBytes > 0 {
		artifact.EstimatedTokens = EstimatedTokens(artifact.EstimatedBytes)
	}
	return artifact
}

// recoverableToolResultArtifact selects a complete, addressable artifact for
// Denova's read-only receipt projection. Artifact creation and verification are
// owned by the public Agent ResultProcessor.
func recoverableToolResultArtifact(artifacts []agentschema.ToolArtifactRef) *agentschema.ToolArtifactRef {
	for index := range artifacts {
		artifact := CanonicalArtifact(artifacts[index])
		if artifact.Complete && artifact.ReadablePath != "" && artifact.ContentType != "" &&
			RecoverableArtifactPurpose(artifact.Purpose) {
			return &artifact
		}
	}
	return nil
}

// EstimatedTokens converts a byte count to the conservative result estimate
// used by retention policy and diagnostics.
func EstimatedTokens(byteSize int64) int {
	if byteSize <= 0 {
		return 0
	}
	estimate := byteSize / 4
	if byteSize%4 != 0 {
		estimate++
	}
	maxInt := int64(^uint(0) >> 1)
	if estimate > maxInt {
		return int(maxInt)
	}
	return int(estimate)
}
