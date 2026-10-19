package goal_test

import (
	"testing"

	"github.com/alfredxw/denova/agent/engine/goal"
	"github.com/alfredxw/denova/agent/engine/goal/goaltest"
)

func TestStandardManagerContract(t *testing.T) {
	goaltest.RunStandardManagerContract(t, func(testing.TB) goal.GoalManager {
		return goal.Standard()
	})
}
