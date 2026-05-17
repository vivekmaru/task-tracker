package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

const defaultQueueLimit int32 = 50

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true)
	mutedStyle    = lipgloss.NewStyle().Faint(true)
)

type TicketLister interface {
	ListTickets(context.Context, services.ListTicketsRequest) ([]db.Ticket, error)
}

type TicketAttemptLister interface {
	ListAttemptsByTicket(context.Context, pgtype.UUID) ([]db.Attempt, error)
}

type Options struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	Status      string
	Type        string
	Limit       int32
}

type QueueModel struct {
	tickets      []db.Ticket
	selected     int
	err          error
	detailCtx    context.Context
	detailLoader TicketAttemptLister
	detail       TicketDetailModel
	showDetail   bool
}

type detailLoadedMsg struct {
	ticket   db.Ticket
	attempts []db.Attempt
	err      error
}

func NewQueueModel(tickets []db.Ticket) QueueModel {
	copied := append([]db.Ticket(nil), tickets...)
	return QueueModel{tickets: copied}
}

func NewQueueModelWithError(err error) QueueModel {
	return QueueModel{err: err}
}

func (m QueueModel) WithDetailLoader(ctx context.Context, loader TicketAttemptLister) QueueModel {
	m.detailCtx = ctx
	m.detailLoader = loader
	return m
}

func LoadQueue(ctx context.Context, lister TicketLister, opts Options) (QueueModel, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = defaultQueueLimit
	}
	tickets, err := lister.ListTickets(ctx, services.ListTicketsRequest{
		WorkspaceID: opts.WorkspaceID,
		ProjectID:   opts.ProjectID,
		Status:      opts.Status,
		Type:        opts.Type,
		Limit:       limit,
	})
	if err != nil {
		return NewQueueModelWithError(err), err
	}
	return NewQueueModel(tickets), nil
}

func Run(ctx context.Context, output io.Writer, lister TicketLister, opts Options) error {
	model, err := LoadQueue(ctx, lister, opts)
	if detailLoader, ok := lister.(TicketAttemptLister); ok {
		model = model.WithDetailLoader(ctx, detailLoader)
	}
	programOptions := []tea.ProgramOption{tea.WithOutput(output), tea.WithContext(ctx)}
	if err != nil {
		programOptions = append(programOptions, tea.WithInput(nil))
	}
	program := tea.NewProgram(model, programOptions...)
	_, runErr := program.Run()
	if runErr != nil {
		return runErr
	}
	return err
}

func (m QueueModel) Init() tea.Cmd {
	if m.err != nil {
		return tea.Quit
	}
	return nil
}

func (m QueueModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.showDetail {
			switch msg.String() {
			case "ctrl+c", "q", "esc":
				return m, tea.Quit
			case "b":
				m.showDetail = false
				return m, nil
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "down", "j":
			return m.MoveDown(), nil
		case "up", "k":
			return m.MoveUp(), nil
		case "enter":
			return m.loadSelectedDetail()
		}
	case detailLoadedMsg:
		if msg.err != nil {
			m.detail = NewTicketDetailModelWithError(msg.ticket, msg.err)
		} else {
			m.detail = NewTicketDetailModel(msg.ticket, msg.attempts)
		}
		m.showDetail = true
		return m, nil
	}
	return m, nil
}

func (m QueueModel) View() string {
	if m.showDetail {
		return m.detail.View()
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Forge Queue"))
	b.WriteString("\n")
	if m.err != nil {
		b.WriteString("Unable to load queue: ")
		b.WriteString(m.err.Error())
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString(summaryLine(m.tickets))
	b.WriteString("\n\n")
	if len(m.tickets) == 0 {
		b.WriteString("No tickets match this queue. Create work or adjust filters.\n")
		b.WriteString(mutedStyle.Render("q quit"))
		b.WriteString("\n")
		return b.String()
	}
	for i, ticket := range m.tickets {
		prefix := " "
		lineStyle := lipgloss.NewStyle()
		if i == m.selected {
			prefix = ">"
			lineStyle = selectedStyle
		}
		b.WriteString(lineStyle.Render(fmt.Sprintf("%s P%d %s %s %s", prefix, ticket.Priority, ticket.Status, ticket.Type, ticket.Title)))
		b.WriteString("\n")
	}
	selected := m.tickets[m.selected]
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Selected"))
	b.WriteString("\n")
	b.WriteString(selected.Title)
	b.WriteString("\n")
	if len(selected.AcceptanceCriteria) > 0 {
		b.WriteString("Acceptance: ")
		b.WriteString(strings.Join(selected.AcceptanceCriteria, "; "))
		b.WriteString("\n")
	}
	if len(selected.RelevantPaths) > 0 {
		b.WriteString("Paths: ")
		b.WriteString(strings.Join(selected.RelevantPaths, ", "))
		b.WriteString("\n")
	}
	b.WriteString(mutedStyle.Render("j/k move  enter open detail later  c copy id later  q quit"))
	b.WriteString("\n")
	return b.String()
}

func (m QueueModel) MoveDown() QueueModel {
	if m.selected < len(m.tickets)-1 {
		m.selected++
	}
	return m
}

func (m QueueModel) MoveUp() QueueModel {
	if m.selected > 0 {
		m.selected--
	}
	return m
}

func (m QueueModel) SelectedIndex() int {
	return m.selected
}

func (m QueueModel) loadSelectedDetail() (QueueModel, tea.Cmd) {
	if len(m.tickets) == 0 || m.detailLoader == nil {
		return m, nil
	}
	ticket := m.tickets[m.selected]
	ctx := m.detailCtx
	if ctx == nil {
		ctx = context.Background()
	}
	return m, func() tea.Msg {
		attempts, err := m.detailLoader.ListAttemptsByTicket(ctx, ticket.ID)
		return detailLoadedMsg{ticket: ticket, attempts: attempts, err: err}
	}
}

func summaryLine(tickets []db.Ticket) string {
	if len(tickets) == 0 {
		return "0 tickets"
	}
	counts := map[string]int{}
	for _, ticket := range tickets {
		counts[ticket.Status]++
	}
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	parts := make([]string, 0, len(statuses)+1)
	parts = append(parts, fmt.Sprintf("%d tickets", len(tickets)))
	for _, status := range statuses {
		parts = append(parts, fmt.Sprintf("%s %d", status, counts[status]))
	}
	return strings.Join(parts, "  ")
}
