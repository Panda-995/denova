// Package modeltask implements bounded, one-shot model workloads outside the
// conversational Agent loop. Product callers own cancellation and persistence,
// including helpers invoked while preparing a conversational turn's commit.
package modeltask
