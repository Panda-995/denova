package toolruntime

import (
	"encoding/json"
	"strings"
	"testing"

	"denova/config"
	agenttool "denova/internal/agents/tool"
	"denova/internal/agents/toolresult"
	producttools "denova/internal/agents/tools"
	imageasset "denova/internal/image/asset"

	agentschema "github.com/alfredxw/denova/agent/schema"
	sdktool "github.com/alfredxw/denova/agent/tool"
)

func TestGeneratedImageToolResultTracksMutationTarget(t *testing.T) {
	payload := imageasset.IllustrationResult{
		Schema:    imageasset.IllustrationResultSchema,
		ImagePath: "assets/writing/ch01--asset_illustration.png",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := producttools.WorkspaceWriteDescriptor(producttools.ToolSourceImage, config.AgentToolImageGeneration, sdktool.ToolRecoveryNonIdempotent)
	record := agenttool.ExecutionRecord{
		ToolName: producttools.GenerateImageToolName, ExecutionID: "call-image", Status: "success", Descriptor: descriptor,
	}
	applyToolMutationReceiptToExecutionRecord(&record, agentschema.TextToolResult(string(raw)))
	mutation, ok := agenttool.MutationFromExecutionRecord(record)
	if !ok || mutation.Source != sdktool.ToolSourceImage || mutation.Target != payload.ImagePath || mutation.PostCheck != sdktool.ToolPostCheckWorkspaceChange {
		t.Fatalf("unexpected mutation: %#v, committed=%t record=%#v", mutation, ok, record)
	}
}

func TestWorkspaceChangeReceiptHidesInternalRevisionsFromModel(t *testing.T) {
	raw := `{"schema":"workspace_change.tool_result.v1","status":"applied","workspace":"/workspace/book-a","change_group_id":"group-1","change_set_id":"change-1","path":"chapters/ch01.md","base_revision":"sha256:before","revision":"sha256:after","review_status":"pending","apply_state":"applied","file_stats":{"bytes":21,"characters":15,"non_whitespace_characters":13,"lines":2}}`
	filtered := toolresult.Filter("edit", `{"path":"chapters/ch01.md","edits":[]}`, raw)
	if !strings.Contains(filtered.Result.ModelContent, `"change_set_id":"change-1"`) {
		t.Fatalf("model receipt lost public change identity: %s", filtered.Result.ModelContent)
	}
	if strings.Contains(filtered.Result.ModelContent, "base_revision") || strings.Contains(filtered.Result.ModelContent, `"revision"`) || strings.Contains(filtered.Result.ModelContent, "sha256:") {
		t.Fatalf("model receipt exposed internal revisions: %s", filtered.Result.ModelContent)
	}
	if !strings.Contains(filtered.Result.ModelContent, `"file_stats":{"bytes":21,"characters":15,"non_whitespace_characters":13,"lines":2}`) {
		t.Fatalf("model receipt lost final file stats: %s", filtered.Result.ModelContent)
	}
}

func TestToolExecutionRecordAssociatesWorkspaceChangeReceipt(t *testing.T) {
	receipt := `{"schema":"workspace_change.tool_result.v1","status":"applied","workspace":"/workspace/book-a","change_group_id":"group-1","change_set_id":"change-1","path":"chapters/ch01.md","base_revision":"sha256:before","revision":"sha256:after","review_status":"pending","apply_state":"applied"}`
	descriptor := producttools.WorkspaceWriteDescriptor(sdktool.ToolSourceWrite, config.AgentToolWorkspaceWrite, sdktool.ToolRecoveryReconcilable)
	record := agenttool.ExecutionRecord{ToolName: "edit", ExecutionID: "call-1", Status: "success", Descriptor: descriptor}
	applyToolMutationReceiptToExecutionRecord(&record, agentschema.ToolResult{Details: []byte(receipt)})
	mutation, ok := agenttool.MutationFromExecutionRecord(record)
	if !ok {
		t.Fatalf("record did not produce a mutation: %#v", record)
	}
	if mutation.Workspace != "/workspace/book-a" || mutation.ChangeGroupID != "group-1" || mutation.ChangeSetID != "change-1" || mutation.Revision != "sha256:after" || mutation.Target != "chapters/ch01.md" {
		t.Fatalf("workspace change identity was not tracked: %#v", mutation)
	}
}

func TestToolExecutionRecordAcceptsPortableWorkspaceChangeReceipt(t *testing.T) {
	receipt := `{"schema":"workspace_change.tool_result.v1","status":"applied","change_group_id":"group-1","change_set_id":"change-1","path":"chapters/ch01.md","base_revision":"sha256:before","revision":"sha256:after","review_status":"pending","apply_state":"applied"}`
	descriptor := producttools.WorkspaceWriteDescriptor(sdktool.ToolSourceWrite, config.AgentToolWorkspaceWrite, sdktool.ToolRecoveryReconcilable)
	record := agenttool.ExecutionRecord{ToolName: "edit", ExecutionID: "call-1", Status: "success", Descriptor: descriptor}
	applyToolMutationReceiptToExecutionRecord(&record, agentschema.ToolResult{Details: []byte(receipt)})
	mutation, ok := agenttool.MutationFromExecutionRecord(record)
	if !ok {
		t.Fatalf("portable receipt did not produce a mutation: %#v", record)
	}
	if mutation.Workspace != "" || mutation.Target != "chapters/ch01.md" || mutation.ChangeGroupID != "group-1" || mutation.ChangeSetID != "change-1" {
		t.Fatalf("portable workspace change identity = %#v", mutation)
	}
}

func TestWorkspaceChangeReceiptUpdatesOnlyTrustedToolExecutionRecords(t *testing.T) {
	content := `{"schema":"workspace_change.tool_result.v1","status":"applied","workspace":"/workspace/book-a","change_group_id":"group-1","change_set_id":"change-1","path":"chapters/ch01.md","base_revision":"sha256:before","revision":"sha256:after","review_status":"pending","apply_state":"applied"}`
	record := agenttool.ExecutionRecord{ToolName: "write"}
	applyWorkspaceChangeReceiptToExecutionRecord(&record, agentschema.ToolResult{Details: []byte(content)})
	if record.Workspace != "/workspace/book-a" || record.ChangeSetID != "change-1" {
		t.Fatalf("execution record lost workspace identity: %#v", record)
	}
	forged := agenttool.ExecutionRecord{ToolName: "read"}
	applyWorkspaceChangeReceiptToExecutionRecord(&forged, agentschema.ToolResult{Details: []byte(content)})
	if forged.Workspace != "" || forged.ChangeSetID != "" {
		t.Fatalf("read forged an execution record receipt: %#v", forged)
	}
}
