package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/braedonsaunders/ether/internal/agent/tools"
	"github.com/braedonsaunders/ether/internal/llm"
)

// loop runs the bounded tool-calling loop for one turn.
func (s *Service) loop(ctx context.Context, p tools.Principal, threadID, turnID string, tc TurnContext, emit func(Chunk) error) (string, error) {
	visible := s.catalogue.Visible(ctx, p)
	defs := make([]llm.ToolDef, 0, len(visible))
	byName := map[string]tools.Tool{}
	for _, t := range visible {
		defs = append(defs, llm.ToolDef{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
		byName[t.Name] = t
	}
	stored, err := s.store.AllMessages(ctx, threadID)
	if err != nil {
		return "error", err
	}
	// Exclude the empty assistant placeholder just appended.
	if len(stored) > 0 && stored[len(stored)-1].ID == turnID {
		stored = stored[:len(stored)-1]
	}
	if len(stored) > 100 {
		stored = stored[len(stored)-100:]
	}
	messages := historyToModel(stored)

	system := systemPrompt
	if tc.Route != "" {
		system += "\n\nThe owner is currently on console route " + tc.Route + "."
		if tc.Record != nil && tc.Record.Type != "" {
			system += fmt.Sprintf(" They are looking at a %s (id %s).", tc.Record.Type, tc.Record.ID)
		}
	}

	var parts []part
	persist := func() {
		_ = s.store.UpdateParts(ctx, turnID, marshalParts(parts))
	}
	tokensUsed := 0
	textSeq := 0

	for step := 0; step < s.maxSteps; step++ {
		if err := emit(chunkStartStep()); err != nil {
			persist()
			return "error", err
		}
		ch, err := s.model.StreamTools(ctx, llm.ToolRequest{
			System: system, Messages: messages, Tools: defs, Temperature: 0.3,
		})
		if err != nil {
			persist()
			_ = emit(chunkFinishStep())
			return "error", err
		}
		var text strings.Builder
		textID := ""
		stepInput, stepOutput := 0, 0
		var calls []llm.ToolCall
		var streamErr error
		for ev := range ch {
			switch ev.Kind {
			case llm.EventTextDelta:
				if textID == "" {
					textSeq++
					textID = fmt.Sprintf("txt_%d", textSeq)
					if err := emit(chunkTextStart(textID)); err != nil {
						streamErr = err
					}
				}
				text.WriteString(ev.Text)
				if emitErr := emit(chunkTextDelta(textID, ev.Text)); emitErr != nil {
					streamErr = emitErr
				}
			case llm.EventToolCallStart:
				_ = emit(chunkToolInputStart(ev.ToolCallID, ev.ToolName))
			case llm.EventToolCallDelta:
				_ = emit(chunkToolInputDelta(ev.ToolCallID, ev.InputDelta))
			case llm.EventToolCall:
				calls = append(calls, llm.ToolCall{ID: ev.ToolCallID, Name: ev.ToolName, Input: ev.Input})
				var in any
				_ = json.Unmarshal(ev.Input, &in)
				if in == nil {
					in = map[string]any{}
				}
				_ = emit(chunkToolInputAvailable(ev.ToolCallID, ev.ToolName, in))
			case llm.EventUsage:
				if ev.InputTokens > 0 {
					stepInput = ev.InputTokens
				}
				if ev.OutputTokens > 0 {
					stepOutput = ev.OutputTokens
				}
			case llm.EventError:
				streamErr = ev.Err
			}
			if streamErr != nil {
				break
			}
		}
		if textID != "" {
			_ = emit(chunkTextEnd(textID))
		}
		if stepInput+stepOutput == 0 {
			tokensUsed += (text.Len() + 3) / 4
		} else {
			tokensUsed += stepInput + stepOutput
		}
		if text.Len() > 0 {
			parts = append(parts, part{"type": "text", "text": text.String()})
		}
		if streamErr != nil {
			persist()
			_ = emit(chunkFinishStep())
			return "error", streamErr
		}
		if len(calls) == 0 {
			persist()
			_ = emit(chunkFinishStep())
			return "stop", nil
		}
		assistantMsg := llm.StreamMessage{Role: "assistant", Content: text.String(), ToolCalls: calls}
		messages = append(messages, assistantMsg)
		var results []llm.ToolResult
		for _, call := range calls {
			res, outPart := s.executeTool(ctx, p, threadID, turnID, byName, call, emit)
			parts = append(parts, outPart)
			// Approval / secret parts are appended right after their tool part.
			if extra, ok := outPart["__extraPart"]; ok {
				delete(outPart, "__extraPart")
				if ep, isPart := extra.(part); isPart {
					parts = append(parts, ep)
				}
			}
			results = append(results, res)
			persist()
		}
		messages = append(messages, llm.StreamMessage{Role: "tool", ToolResults: results})
		persist()
		if err := emit(chunkFinishStep()); err != nil {
			return "error", err
		}
		if tokensUsed > s.budget {
			_ = emit(chunkError("This turn reached its token budget and stopped. Ask a narrower question."))
			return "other", nil
		}
	}
	return "tool-calls", nil
}

// executeTool validates, runs, audits and renders one tool call. It returns
// the model-facing result and the stored UI part. A proposal or secret
// request adds an extra part under the "__extraPart" key (stripped by caller).
func (s *Service) executeTool(ctx context.Context, p tools.Principal, threadID, turnID string, byName map[string]tools.Tool, call llm.ToolCall, emit func(Chunk) error) (llm.ToolResult, part) {
	toolPart := part{
		"type": "tool-" + call.Name, "toolCallId": call.ID, "state": "input-available",
	}
	var input map[string]any
	if json.Unmarshal(call.Input, &input) != nil || input == nil {
		input = map[string]any{}
		toolPart["input"] = input
		if len(call.Input) > 0 && string(call.Input) != "{}" {
			return s.toolError(ctx, p, threadID, call, toolPart, emit,
				tools.Userf("Tool input was not a JSON object."))
		}
	} else {
		toolPart["input"] = input
	}
	tool, ok := byName[call.Name]
	if !ok {
		// Re-check against the live catalogue (visibility may have changed).
		tool, ok = s.catalogue.Lookup(ctx, p, call.Name)
		if !ok {
			return s.toolError(ctx, p, threadID, call, toolPart, emit,
				tools.Userf("Tool %q is not available.", call.Name))
		}
	}
	if err := validateInput(tool.InputSchema, input); err != nil {
		return s.toolError(ctx, p, threadID, call, toolPart, emit,
			tools.Userf("Invalid tool input: %v", err))
	}
	start := time.Now()
	result, err := tool.Execute(ctx, p, input)
	dur := time.Since(start)
	if err != nil {
		_ = s.store.Audit(ctx, threadID, call.Name, principalLabel(p), "error", tools.SafeMessage(err), dur)
		s.log.Debug("agent tool failed", "tool", call.Name, "err", err)
		msg := tools.SafeMessage(err)
		toolPart["state"] = "output-error"
		toolPart["errorText"] = msg
		_ = emit(chunkToolOutputError(call.ID, msg))
		return llm.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: mustJSON(map[string]any{"error": msg}), IsError: true}, toolPart
	}

	output := map[string]any{}
	for k, v := range result.Data {
		output[k] = v
	}
	status := "ok"

	if result.Proposal != nil {
		prop := result.Proposal
		details, _ := json.Marshal(prop.Details)
		if prop.Details == nil {
			details = json.RawMessage(`[]`)
		}
		approvalInput := input
		if len(prop.Input) > 0 {
			approvalInput = prop.Input
		}
		appr, aerr := s.store.CreateApproval(ctx, Approval{
			ThreadID: threadID, MessageID: turnID, ToolName: call.Name, Input: approvalInput,
			Category: prop.Category, Description: prop.Description, Details: details,
		})
		status = "proposal"
		if aerr == nil {
			detailParts := []any{}
			for _, d := range prop.Details {
				detailParts = append(detailParts, map[string]any{"label": d.Label, "value": d.Value})
			}
			toolPart["__extraPart"] = part{
				"type": "approval-request", "approvalId": appr.ID,
				"categoryLabel": prop.Category, "description": prop.Description,
				"details": detailParts, "status": "pending",
			}
			output["status"] = "pending_approval"
			output["approvalId"] = appr.ID
			output["message"] = "This change is waiting for the owner's approval on an approval card. It has not been applied. Tell the owner it is waiting for approval."
		} else {
			output["status"] = "proposal_failed"
			s.log.Warn("agent approval store failed", "tool", call.Name, "err", aerr)
		}
	}
	if result.Secret != nil {
		sec := result.Secret
		req, serr := s.store.CreateSecret(ctx, SecretReq{
			ThreadID: threadID, MessageID: turnID,
			ProviderLabel: sec.ProviderLabel, CredentialLabel: sec.CredentialLabel,
			Purpose: sec.Purpose, HelpURL: sec.HelpURL,
		})
		status = "secret_request"
		if serr == nil {
			s.mu.Lock()
			s.secrets[req.ID] = sec.Store
			s.mu.Unlock()
			sp := part{
				"type": "secret-request", "requestId": req.ID,
				"providerLabel": sec.ProviderLabel, "credentialLabel": sec.CredentialLabel,
				"purpose": sec.Purpose, "status": "pending",
			}
			if sec.HelpURL != "" {
				sp["helpUrl"] = sec.HelpURL
			}
			toolPart["__extraPart"] = sp
			output["status"] = "secret_requested"
			output["message"] = "A secret request card is waiting for the owner. The value goes straight to sealed storage and will never be shown in chat. Tell the owner to use the card."
		} else {
			output["status"] = "secret_request_failed"
			s.log.Warn("agent secret store failed", "tool", call.Name, "err", serr)
		}
	}
	if result.Navigate != "" {
		output["navigate"] = result.Navigate
	}
	if len(result.Links) > 0 {
		output["links"] = result.Links
	}
	if tool.Summarize != nil && len(result.Data) == 0 {
		output["summary"] = tool.Summarize(result)
	}
	_ = s.store.Audit(ctx, threadID, call.Name, principalLabel(p), status, "", dur)
	toolPart["state"] = "output-available"
	toolPart["output"] = output
	_ = emit(chunkToolOutputAvailable(call.ID, output))
	return llm.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: truncateOutput([]byte(mustJSON(output)))}, toolPart
}

func (s *Service) toolError(ctx context.Context, p tools.Principal, threadID string, call llm.ToolCall, toolPart part, emit func(Chunk) error, err error) (llm.ToolResult, part) {
	msg := tools.SafeMessage(err)
	_ = s.store.Audit(ctx, threadID, call.Name, principalLabel(p), "error", msg, 0)
	toolPart["state"] = "output-error"
	toolPart["errorText"] = msg
	_ = emit(chunkToolOutputError(call.ID, msg))
	return llm.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: mustJSON(map[string]any{"error": msg}), IsError: true}, toolPart
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// DecideApproval approves or rejects a stored proposal. Approval re-runs the
// tool's Execute with the stored input to rebuild the proposal closure, then
// applies it. The stored part is patched to the final status.
func (s *Service) DecideApproval(ctx context.Context, p tools.Principal, id, decision, note string) (Approval, error) {
	appr, ok, err := s.store.GetApproval(ctx, id)
	if err != nil {
		return appr, err
	}
	if !ok {
		return appr, fmt.Errorf("approval not found")
	}
	if appr.Status != "pending" {
		return appr, fmt.Errorf("approval is already %s", appr.Status)
	}
	appr.DecisionNote = note
	switch decision {
	case "rejected":
		appr.Status = "rejected"
	case "approved":
		err = s.applyApproval(ctx, p, &appr)
		if err != nil {
			appr.Status = "failed"
			appr.FailureReason = tools.SafeMessage(err)
		} else {
			appr.Status = "approved"
		}
	default:
		return appr, fmt.Errorf("decision must be approved or rejected")
	}
	if err := s.store.UpdateApproval(ctx, appr); err != nil {
		return appr, err
	}
	s.patchPart(ctx, appr.ThreadID, appr.MessageID, "approval-request", "approvalId", appr.ID, map[string]any{
		"status": appr.Status, "decisionNote": appr.DecisionNote, "failureReason": appr.FailureReason,
	})
	return appr, nil
}

func (s *Service) applyApproval(ctx context.Context, p tools.Principal, appr *Approval) error {
	tool, ok := s.catalogue.Lookup(ctx, p, appr.ToolName)
	if !ok {
		return tools.Userf("The tool for this change is no longer available.")
	}
	input := appr.Input
	if input == nil {
		input = map[string]any{}
	}
	if err := validateInput(tool.InputSchema, input); err != nil {
		return tools.Userf("The stored change no longer validates: %v", err)
	}
	start := time.Now()
	result, err := tool.Execute(ctx, p, input)
	if err != nil {
		_ = s.store.Audit(ctx, appr.ThreadID, appr.ToolName, principalLabel(p), "error", tools.SafeMessage(err), time.Since(start))
		return err
	}
	if result.Secret != nil {
		sec := result.Secret
		if sec.Store == nil {
			return tools.Userf("The secret request is no longer available.")
		}
		req, err := s.store.CreateSecret(ctx, SecretReq{ThreadID: appr.ThreadID, MessageID: appr.MessageID, ProviderLabel: sec.ProviderLabel, CredentialLabel: sec.CredentialLabel, Purpose: sec.Purpose, HelpURL: sec.HelpURL})
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.secrets[req.ID] = sec.Store
		s.mu.Unlock()
		parts := []map[string]any{{"type": "secret-request", "requestId": req.ID, "providerLabel": sec.ProviderLabel, "credentialLabel": sec.CredentialLabel, "purpose": sec.Purpose, "status": "pending"}}
		raw, err := json.Marshal(parts)
		if err != nil {
			return err
		}
		_, err = s.store.AppendMessage(ctx, appr.ThreadID, "assistant", raw)
		return err
	}
	if result.Proposal == nil || result.Proposal.Apply == nil {
		_ = s.store.Audit(ctx, appr.ThreadID, appr.ToolName, principalLabel(p), "error", "proposal could not be rebuilt", time.Since(start))
		return tools.Userf("The change could not be rebuilt. Ask again to create a fresh proposal.")
	}
	applied, err := result.Proposal.Apply(ctx)
	_ = applied
	status := "approved"
	mapErr := ""
	if err != nil {
		status = "apply_failed"
		mapErr = tools.SafeMessage(err)
	}
	_ = s.store.Audit(ctx, appr.ThreadID, appr.ToolName, principalLabel(p), status, mapErr, time.Since(start))
	return err
}

// SubmitSecret stores a submitted credential through the request's Store
// closure. The value is never logged, stored in a message or returned.
func (s *Service) SubmitSecret(ctx context.Context, id, value string) error {
	req, ok, err := s.store.GetSecret(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("secret request not found")
	}
	if req.Status != "pending" {
		return fmt.Errorf("secret request is already %s", req.Status)
	}
	s.mu.Lock()
	storeFn := s.secrets[id]
	s.mu.Unlock()
	if storeFn == nil {
		// Closure did not survive a restart; expire the durable request.
		_ = s.store.UpdateSecretStatus(ctx, id, "expired")
		s.patchPart(ctx, req.ThreadID, req.MessageID, "secret-request", "requestId", id, map[string]any{"status": "expired"})
		return fmt.Errorf("secret request has expired")
	}
	if err := storeFn(ctx, value); err != nil {
		s.log.Warn("agent secret store failed", "request", id, "err", tools.SafeMessage(err))
		return fmt.Errorf("could not store the secret")
	}
	s.mu.Lock()
	delete(s.secrets, id)
	s.mu.Unlock()
	if err := s.store.UpdateSecretStatus(ctx, id, "stored"); err != nil {
		return err
	}
	s.patchPart(ctx, req.ThreadID, req.MessageID, "secret-request", "requestId", id, map[string]any{"status": "stored"})
	return nil
}

// patchPart updates fields of one stored part identified by type + id key.
func (s *Service) patchPart(ctx context.Context, threadID, messageID, partType, idKey, id string, fields map[string]any) {
	msgs, err := s.store.AllMessages(ctx, threadID)
	if err != nil {
		return
	}
	for _, m := range msgs {
		if m.ID != messageID {
			continue
		}
		var parts []map[string]any
		if json.Unmarshal(m.Parts, &parts) != nil {
			return
		}
		changed := false
		for _, pt := range parts {
			if pt["type"] == partType && pt[idKey] == id {
				for k, v := range fields {
					if s, isStr := v.(string); isStr && s == "" {
						continue
					}
					pt[k] = v
				}
				changed = true
			}
		}
		if changed {
			b, _ := json.Marshal(parts)
			_ = s.store.UpdateParts(ctx, messageID, b)
		}
		return
	}
}
