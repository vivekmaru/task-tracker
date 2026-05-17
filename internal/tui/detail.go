package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

type TicketDetailModel struct {
	ticket   db.Ticket
	timeline TicketTimeline
	err      error
}

type TicketTimeline struct {
	Attempts    []db.Attempt
	Checkpoints []db.AttemptCheckpoint
	Events      []db.TicketEvent
	Artifacts   []db.Artifact
}

func NewTicketDetailModel(ticket db.Ticket, attempts []db.Attempt) TicketDetailModel {
	copied := append([]db.Attempt(nil), attempts...)
	return NewTicketDetailModelWithTimeline(ticket, TicketTimeline{Attempts: copied})
}

func NewTicketDetailModelWithTimeline(ticket db.Ticket, timeline TicketTimeline) TicketDetailModel {
	return TicketDetailModel{
		ticket: ticket,
		timeline: TicketTimeline{
			Attempts:    append([]db.Attempt(nil), timeline.Attempts...),
			Checkpoints: append([]db.AttemptCheckpoint(nil), timeline.Checkpoints...),
			Events:      append([]db.TicketEvent(nil), timeline.Events...),
			Artifacts:   append([]db.Artifact(nil), timeline.Artifacts...),
		},
	}
}

func NewTicketDetailModelWithError(ticket db.Ticket, err error) TicketDetailModel {
	return TicketDetailModel{ticket: ticket, err: err}
}

func (m TicketDetailModel) View() string {
	var b strings.Builder
	ticketID := uuidText(m.ticket.ID)
	b.WriteString(titleStyle.Render("Ticket Detail"))
	b.WriteString("\n")
	b.WriteString(m.ticket.Title)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%s %s P%d", valueOrDash(m.ticket.Status), valueOrDash(m.ticket.Type), m.ticket.Priority))
	b.WriteString("\n")
	writeJoinedLine(&b, "Tags", m.ticket.Tags)
	b.WriteString(fmt.Sprintf("Source: %s %s\n", valueOrDash(m.ticket.CreatedBy), textValue(m.ticket.CreatedByID)))
	if m.ticket.SourceAttemptID.Valid {
		b.WriteString("Source attempt: ")
		b.WriteString(uuidText(m.ticket.SourceAttemptID))
		b.WriteString("\n")
	}
	if m.ticket.CreationReason.Valid {
		b.WriteString("Reason: ")
		b.WriteString(m.ticket.CreationReason.String)
		b.WriteString("\n")
	}
	if m.ticket.Description != "" {
		b.WriteString("\n")
		b.WriteString(m.ticket.Description)
		b.WriteString("\n")
	}
	if m.err != nil {
		b.WriteString("\n")
		b.WriteString("Unable to load attempts and timeline: ")
		b.WriteString(m.err.Error())
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("b back  q quit"))
		b.WriteString("\n")
		return b.String()
	}
	writeListSection(&b, "Acceptance", m.ticket.AcceptanceCriteria, "- ")
	writeListSection(&b, "Verification", decodeStringArray(m.ticket.VerificationCommands), "$ ")
	writeListSection(&b, "Paths", m.ticket.RelevantPaths, "- ")
	writeListSection(&b, "Expected artifacts", m.ticket.ExpectedArtifacts, "- ")
	writeListSection(&b, "Required tools", m.ticket.RequiredTools, "- ")
	writeListSection(&b, "Permissions", m.ticket.RequiredPermissions, "- ")
	writeListSection(&b, "Capabilities", m.ticket.RequiredCapabilities, "- ")
	writeListSection(&b, "Harnesses", m.ticket.AllowedHarnesses, "- ")
	writeAttemptSections(&b, m.timeline.Attempts)
	writeCheckpointTimeline(&b, m.timeline.Checkpoints)
	writeEventTimeline(&b, m.timeline.Events)
	writeArtifactTimeline(&b, m.timeline.Artifacts)
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Copy"))
	b.WriteString("\n")
	b.WriteString("Ticket ID: ")
	b.WriteString(ticketID)
	b.WriteString("\n")
	b.WriteString("forge get --id ")
	b.WriteString(ticketID)
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("b back  q quit"))
	b.WriteString("\n")
	return b.String()
}

func writeAttemptSections(b *strings.Builder, attempts []db.Attempt) {
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Attempts timeline"))
	b.WriteString("\n")
	if len(attempts) == 0 {
		b.WriteString("No attempts recorded yet.\n")
		return
	}
	currentIndex := currentAttemptIndex(attempts)
	b.WriteString(titleStyle.Render("Current attempt"))
	b.WriteString("\n")
	writeAttemptLine(b, attempts[currentIndex])
	writeAttemptNotes(b, attempts[currentIndex])
	priorCount := 0
	for i, attempt := range attempts {
		if i == currentIndex {
			continue
		}
		if priorCount == 0 {
			b.WriteString("\n")
			b.WriteString(titleStyle.Render("Prior attempts"))
			b.WriteString("\n")
		}
		writeAttemptLine(b, attempt)
		writeAttemptNotes(b, attempt)
		priorCount++
	}
}

func currentAttemptIndex(attempts []db.Attempt) int {
	return 0
}

func writeAttemptLine(b *strings.Builder, attempt db.Attempt) {
	label := attemptStateLabel(attempt.Status)
	if label == "blocked" {
		b.WriteString(fmt.Sprintf("%s %s/%s", label, valueOrDash(attempt.AgentID), valueOrDash(attempt.Model)))
	} else {
		b.WriteString(fmt.Sprintf("%s %s %s/%s", label, valueOrDash(attempt.Status), valueOrDash(attempt.AgentID), valueOrDash(attempt.Model)))
	}
	if attempt.ProgressPercent > 0 {
		b.WriteString(fmt.Sprintf(" %d%%", attempt.ProgressPercent))
	}
	b.WriteString("\n")
}

func writeAttemptNotes(b *strings.Builder, attempt db.Attempt) {
	if attempt.CurrentSummary.Valid {
		b.WriteString(attempt.CurrentSummary.String)
		b.WriteString("\n")
	}
	if attempt.NextStep.Valid {
		b.WriteString("Next: ")
		b.WriteString(attempt.NextStep.String)
		b.WriteString("\n")
	}
	if attempt.FailureReason.Valid {
		b.WriteString("Failure: ")
		b.WriteString(attempt.FailureReason.String)
		if attempt.FailureCategory.Valid {
			b.WriteString(" (")
			b.WriteString(attempt.FailureCategory.String)
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	if blocker := timelineReason(attempt.Blocker); blocker != "" {
		b.WriteString("Blocker: ")
		b.WriteString(blocker)
		b.WriteString("\n")
	}
}

func writeCheckpointTimeline(b *strings.Builder, checkpoints []db.AttemptCheckpoint) {
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Checkpoints timeline"))
	b.WriteString("\n")
	if len(checkpoints) == 0 {
		b.WriteString("No checkpoints recorded.\n")
		return
	}
	for _, checkpoint := range checkpoints {
		b.WriteString("checkpoint ")
		b.WriteString(checkpoint.Summary)
		b.WriteString("\n")
		writeJoinedLine(b, "Commands", checkpoint.CommandsRun)
		writeJoinedLine(b, "Files", checkpoint.FilesTouched)
		if checkpoint.NextStep.Valid {
			b.WriteString("Next: ")
			b.WriteString(checkpoint.NextStep.String)
			b.WriteString("\n")
		}
		if checkpoint.Risk.Valid {
			b.WriteString("Risk: ")
			b.WriteString(checkpoint.Risk.String)
			b.WriteString("\n")
		}
	}
}

func writeEventTimeline(b *strings.Builder, events []db.TicketEvent) {
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Events timeline"))
	b.WriteString("\n")
	if len(events) == 0 {
		b.WriteString("No ticket events recorded.\n")
		return
	}
	for _, event := range events {
		b.WriteString(event.Type)
		b.WriteString(" by ")
		b.WriteString(valueOrDash(event.ActorType))
		b.WriteString("/")
		b.WriteString(textValue(event.ActorID))
		b.WriteString("\n")
		if reason := timelineReason(event.Data); reason != "" {
			b.WriteString("Data: ")
			b.WriteString(reason)
			b.WriteString("\n")
		}
	}
}

func writeArtifactTimeline(b *strings.Builder, artifacts []db.Artifact) {
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Proof artifacts"))
	b.WriteString("\n")
	if len(artifacts) == 0 {
		b.WriteString("No proof artifacts recorded.\n")
		return
	}
	for _, artifact := range artifacts {
		b.WriteString(valueOrDash(artifact.Role))
		b.WriteString(" ")
		b.WriteString(valueOrDash(artifact.Type))
		b.WriteString(" ")
		b.WriteString(valueOrDash(artifact.Name))
		b.WriteString("\n")
		if artifact.Url != "" {
			b.WriteString(artifact.Url)
			b.WriteString("\n")
		}
	}
}

func attemptStateLabel(status string) string {
	switch status {
	case "running":
		return "active"
	case "blocked":
		return "blocked"
	case "succeeded", "failed", "expired", "cancelled":
		return "terminal"
	default:
		return "state"
	}
}

func writeListSection(b *strings.Builder, title string, values []string, prefix string) {
	if len(values) == 0 {
		return
	}
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")
	for _, value := range values {
		b.WriteString(prefix)
		b.WriteString(value)
		b.WriteString("\n")
	}
}

func writeJoinedLine(b *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		return
	}
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(strings.Join(values, ", "))
	b.WriteString("\n")
}

func decodeStringArray(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func timelineReason(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return string(raw)
	}
	for _, key := range []string{"reason", "summary", "message", "detail"} {
		if value, ok := data[key].(string); ok && value != "" {
			return value
		}
	}
	return string(raw)
}

func textValue(value pgtype.Text) string {
	if !value.Valid || value.String == "" {
		return "-"
	}
	return value.String
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func uuidText(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	b := id.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
