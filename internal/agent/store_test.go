package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/braedonsaunders/ether/internal/store"
)

func TestStoreSQLiteMigrationsAndPaging(t *testing.T) {
	db := store.OpenTest(t) // applies every registered migration, including "agent"
	s := NewStore(db)
	ctx := context.Background()
	th, err := s.CreateThread(ctx, "First")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		parts, _ := json.Marshal([]map[string]any{{"type": "text", "text": "m"}})
		if _, err := s.AppendMessage(ctx, th.ID, "user", parts); err != nil {
			t.Fatal(err)
		}
	}
	page, hasOlder, err := s.Messages(ctx, th.ID, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || !hasOlder {
		t.Fatalf("page len %d hasOlder %v", len(page), hasOlder)
	}
	if page[0].seq >= page[1].seq {
		t.Error("page not ascending")
	}
	older, hasOlder, err := s.Messages(ctx, th.ID, page[0].ID, 10)
	if err != nil || len(older) != 3 || hasOlder {
		t.Fatalf("older len %d hasOlder %v err %v", len(older), hasOlder, err)
	}
	appr, err := s.CreateApproval(ctx, Approval{ThreadID: th.ID, MessageID: page[0].ID, ToolName: "x_tool",
		Input: map[string]any{"a": "b"}, Category: "Cat", Description: "Desc"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetApproval(ctx, appr.ID)
	if err != nil || !ok || got.Input["a"] != "b" || got.Status != "pending" {
		t.Fatalf("approval %+v ok %v err %v", got, ok, err)
	}
	if err := s.Audit(ctx, th.ID, "x_tool", "console:viewer:0", "ok", "", 3*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	var auditN int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_audit`).Scan(&auditN); err != nil || auditN != 1 {
		t.Fatalf("audit %d %v", auditN, err)
	}
	// Audit never carries inputs: schema has no input column.
	var cols string
	rows, _ := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('agent_audit')`)
	defer rows.Close()
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		cols += c + ","
	}
	if strings.Contains(cols, "input") {
		t.Fatalf("agent_audit must not store inputs: %s", cols)
	}
	if err := s.DeleteThread(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Thread(ctx, th.ID); ok {
		t.Error("thread survived delete")
	}
}

// Golden encoder transcript: regenerate the golden fixture by running the
// loop and encoding, then compare chunk types in order.
func TestEncoderChunkOrder(t *testing.T) {
	chunks := []Chunk{
		chunkStart("msg_golden"), chunkStartStep(),
		chunkTextStart("txt_1"), chunkTextDelta("txt_1", "Checking the server. "), chunkTextEnd("txt_1"),
		chunkToolInputStart("call_1", "server_status"), chunkToolInputDelta("call_1", "{}"),
		chunkToolInputAvailable("call_1", "server_status", map[string]any{}),
		chunkToolOutputAvailable("call_1", map[string]any{"healthy": true}),
		chunkFinishStep(), chunkStartStep(),
		chunkTextStart("txt_2"), chunkTextDelta("txt_2", "The server is healthy."), chunkTextEnd("txt_2"),
		chunkFinishStep(), chunkFinish("stop"),
	}
	var sb strings.Builder
	for _, c := range chunks {
		b, _ := json.Marshal(c)
		sb.WriteString("data: " + string(b) + "\n\n")
	}
	sb.WriteString("data: [DONE]\n\n")
	got := sb.String()
	for _, want := range []string{`"type":"start"`, `"messageId":"msg_golden"`, `"inputTextDelta":"{}"`, `"toolCallId":"call_1"`, `"finishReason":"stop"`} {
		if !strings.Contains(got, want) {
			t.Errorf("golden missing %s", want)
		}
	}
}
