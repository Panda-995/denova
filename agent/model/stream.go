package model

import (
	"errors"
	"io"

	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// ConcatMessageStream drains and strictly merges a message stream.
func ConcatMessageStream(stream *agentstream.StreamReader[*agentschema.Message]) (*agentschema.Message, error) {
	defer stream.Close()
	assembler := agentschema.NewMessageAssembler()
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return assembler.Message()
		}
		if err != nil {
			return nil, err
		}
		if err := assembler.Append(message); err != nil {
			return nil, err
		}
	}
}
