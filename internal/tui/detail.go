package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

type TicketDetailModel struct {
	ticket   db.Ticket
	attempts []db.Attempt
	err      error
}

func NewTicketDetailModel(ticket db.Ticket, attempts []db.Attempt) TicketDetailModel {
	copied := append([]db.Attempt(nil), attempts...)
	return TicketDetailModel{ticket: ticket, attempts: copied}
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
		b.WriteString("Unable to load attempts: ")
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
	writeAttemptSections(&b, m.attempts)
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Copy"))
	b.WriteString("\n")
	b.WriteString("Ticket ID: ")
	b.WriteString(ticketID)
	b.WriteString("\n")
	b.WriteString("forge get --ticket-id ")
	b.WriteString(ticketID)
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("b back  q quit"))
	b.WriteString("\n")
	return b.String()
}

func writeAttemptSections(b *strings.Builder, attempts []db.Attempt) {
	if len(attempts) == 0 {
		b.WriteString("\n")
		b.WriteString(titleStyle.Render("Attempts"))
		b.WriteString("\n")
		b.WriteString("No attempts recorded yet.\n")
		return
	}
	currentIndex := currentAttemptIndex(attempts)
	b.WriteString("\n")
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
	for i, attempt := range attempts {
		if attempt.Status == services.AttemptStatusRunning || attempt.Status == services.AttemptStatusBlocked {
			return i
		}
	}
	return 0
}

func writeAttemptLine(b *strings.Builder, attempt db.Attempt) {
	b.WriteString(fmt.Sprintf("%s %s/%s", valueOrDash(attempt.Status), valueOrDash(attempt.AgentID), valueOrDash(attempt.Model)))
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
