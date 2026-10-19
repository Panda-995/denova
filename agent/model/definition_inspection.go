package model

import (
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// ModelRequestInspection is a detached, non-executable view of the exact
// provider-neutral request assembled by Session.Inspect. It intentionally does
// not expose the concrete model adapter or a Generate/Stream method.
type ModelRequestInspection struct {
	Messages             []*agentschema.Message
	Options              Options
	Streaming            bool
	StablePrefixMessages int
	inputEstimator       InputEstimator
}

// EstimateInput uses the same captured model policy as the inspected call.
func (request ModelRequestInspection) EstimateInput() (InputSize, error) {
	return request.inputEstimator.Estimate(request.Messages, request.Options.Tools)
}

func InspectModelRequest(snapshot *ModelRequestSnapshot) ModelRequestInspection {
	if snapshot == nil {
		return ModelRequestInspection{}
	}
	options := snapshot.ResolvedOptions()
	return ModelRequestInspection{
		Messages:             snapshot.Messages(),
		Options:              *options,
		Streaming:            snapshot.Streaming(),
		StablePrefixMessages: snapshot.StablePrefixMessages(),
		inputEstimator:       snapshot.inputEstimator,
	}
}
