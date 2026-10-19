package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"denova/config"
	"denova/internal/agents/conversationconfig"
	appsvc "denova/internal/app"
	"denova/internal/project"
	"github.com/cloudwego/hertz/pkg/app"
)

func TestAgentChatOnlySessionDeletionIsLocalizedClientError(t *testing.T) {
	root := t.TempDir()
	application, err := appsvc.New(context.Background(), &config.Config{
		OpenAIModel: "test-model", NovaDir: root, Workspace: root,
		OpenAIBaseURL: "http://127.0.0.1:1/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Close)
	handler := &Handlers{app: application}
	for locale, want := range map[string]string{
		"zh-CN": "不能删除当前项目的唯一会话。",
		"en-US": "The only conversation in this project cannot be deleted.",
	} {
		t.Run(locale, func(t *testing.T) {
			request := app.NewContext(0)
			request.Request.Header.Set("Content-Type", "application/json")
			request.Request.Header.Set("X-Denova-Locale", locale)
			request.Request.SetBodyString(fmt.Sprintf(`{"session_id":%q}`, application.Session().ID))
			request.Set(projectScopeContextKey, project.Layout{ProjectID: application.ProjectID()})
			handler.HandleAgentChatSessionDelete(context.Background(), request)
			var body agentRuntimeErrorResponse
			if err := json.Unmarshal(request.Response.Body(), &body); err != nil {
				t.Fatal(err)
			}
			if request.Response.StatusCode() != 409 || body.Code != "api.session.onlySession" || body.Error != want {
				t.Fatalf("delete rejection status=%d body=%+v", request.Response.StatusCode(), body)
			}
		})
	}
}

func TestAgentCommandUnsupportedCapabilityIsLocalizedClientError(t *testing.T) {
	for locale, want := range map[string]string{
		"zh-CN": "当前引擎不支持此操作或参数。",
		"en-US": "This runtime does not support the requested operation or parameters.",
	} {
		t.Run(locale, func(t *testing.T) {
			c := app.NewContext(0)
			c.Request.Header.Set("X-Denova-Locale", locale)
			h := &Handlers{}
			h.writeAgentCommandError(context.Background(), c, fmt.Errorf("suspend: %w", conversationconfig.ErrRuntimeCapabilityUnsupported), "external-test")
			var body agentRuntimeErrorResponse
			if err := json.Unmarshal(c.Response.Body(), &body); err != nil {
				t.Fatal(err)
			}
			if c.Response.StatusCode() != 400 || body.Code != "agent_runtime.capability_unsupported" || body.Error != want || body.Details["target_operation_id"] != "external-test" {
				t.Fatalf("unexpected response: %d %s", c.Response.StatusCode(), c.Response.Body())
			}
		})
	}
}

func TestWritingAgentCommandKindIncludesQueueControls(t *testing.T) {
	t.Parallel()

	tests := map[string]appsvc.CommandKind{
		"steer":         appsvc.CommandSteer,
		"follow_up":     appsvc.CommandFollowUp,
		"next_turn":     appsvc.CommandNextTurn,
		"abort":         appsvc.CommandAbort,
		"steer_queued":  appsvc.CommandSteerQueued,
		"cancel_queued": appsvc.CommandCancelQueued,
	}
	for input, want := range tests {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got, err := writingAgentCommandKind(input)
			if err != nil || got != want {
				t.Fatalf("writingAgentCommandKind(%q) = %q, %v; want %q", input, got, err, want)
			}
		})
	}
	if got, err := writingAgentCommandKind("queue"); err == nil {
		t.Fatalf("writingAgentCommandKind(queue) = %q, want error", got)
	}
}
