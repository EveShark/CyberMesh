package main

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const monitorRefreshInterval = 5 * time.Second

type monitorModel struct {
	ctx          context.Context
	fetch        monitorFetcher
	autoExit     bool
	loading      bool
	data         monitorData
	err          error
	showHelp     bool
	pane         int
	outboxIx     int
	ackIx        int
	scrollOffset int
	width        int
	height       int
}

func newMonitorModel(ctx context.Context, fetch monitorFetcher) tea.Model {
	width, height := initialTerminalSize()
	return &monitorModel{
		ctx:      ctx,
		fetch:    fetch,
		autoExit: tuiAutoExitEnabled(),
		loading:  true,
		width:    width,
		height:   height,
	}
}

func (m *monitorModel) Init() tea.Cmd {
	return tea.Batch(
		asyncMonitorLoad(m.ctx, m.fetch),
		monitorRefreshTick(monitorRefreshInterval),
	)
}

func (m *monitorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		if m.showHelp {
			switch msg.String() {
			case "?", "esc":
				m.showHelp = false
			}
			return m, nil
		}
		if handleViewportKey(msg, m.height, &m.scrollOffset) {
			return m, nil
		}
		switch msg.String() {
		case "?":
			m.showHelp = true
			return m, nil
		case "ctrl+c", "q":
			return m, tea.Quit
		case "r":
			m.loading = true
			m.err = nil
			m.scrollOffset = 0
			return m, asyncMonitorLoad(m.ctx, m.fetch)
		case "tab":
			if len(m.data.Outbox.Rows) > 0 && len(m.data.Acks.Rows) > 0 {
				m.pane = (m.pane + 1) % 2
			}
		case "up", "k":
			if m.pane == 0 && m.outboxIx > 0 {
				m.outboxIx--
			}
			if m.pane == 1 && m.ackIx > 0 {
				m.ackIx--
			}
		case "down", "j":
			if m.pane == 0 && m.outboxIx < len(m.data.Outbox.Rows)-1 {
				m.outboxIx++
			}
			if m.pane == 1 && m.ackIx < len(m.data.Acks.Rows)-1 {
				m.ackIx++
			}
		}
	case monitorLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.data = msg.data
		}
		if m.autoExit {
			return m, tea.Quit
		}
	case refreshTickMsg:
		return m, tea.Batch(
			asyncMonitorLoad(m.ctx, m.fetch),
			monitorRefreshTick(monitorRefreshInterval),
		)
	}
	return m, nil
}

func (m *monitorModel) View() string {
	compact := compactLayout(m.width)
	tiny := tinyLayout(m.width)
	header := []string{
		titleStyle.Render("CyberMesh Monitor"),
		helpStyle.Render(monitorHelpText(compact)),
		"",
		kvLine("Leader", shortID(m.data.Consensus.Leader, 20)),
		kvLine("Leader ID", shortID(m.data.Consensus.LeaderID, 20)),
		kvLine("Term", fmt.Sprintf("%d", m.data.Consensus.Term)),
		kvLine("Phase", renderStatus(m.data.Consensus.Phase)),
		kvLine("Outbox Rows", fmt.Sprintf("%d", len(m.data.Outbox.Rows))),
		kvLine("ACK Rows", fmt.Sprintf("%d", len(m.data.Acks.Rows))),
	}
	if m.loading {
		return joinLines(append(header, "", "Loading monitor data...")...)
	}
	if m.showHelp {
		return renderHelpOverlay("Monitor Help", []string{
			"j/k or arrows: move inside active pane",
			"tab: switch between outbox and ack panes",
			"PgUp / PgDn: scroll the full detail body",
			"Ctrl+U / Ctrl+D: faster full-body scroll",
			"r: refresh",
			"q: quit",
		}, m.height)
	}
	if m.err != nil {
		return joinLines(append(header, "", errorStyle.Render(m.err.Error()))...)
	}
	body := []string{""}
	body = append(body, renderMonitorOutboxList(m.data.Outbox.Rows, m.outboxIx, m.pane == 0, listRowsForHeight(m.height), compact)...)
	if !tiny {
		body = append(body, "")
		body = append(body, renderMonitorAckList(m.data.Acks.Rows, m.ackIx, m.pane == 1, listRowsForHeight(m.height), compact)...)
	}
	body = append(body, "")
	body = append(body, labelStyle.Render("Selected Details"))
	body = append(body, m.selectedDetails())
	return finalizeView(header, body, m.height, m.scrollOffset)
}

func monitorHelpText(compact bool) string {
	if compact {
		return "j/k move • ? help • tab pane • pg scroll • r refresh • q quit"
	}
	return "j/k move • ? help • tab switch pane • pgup/pgdn scroll • r refresh • q quit"
}

func (m *monitorModel) selectedDetails() string {
	if m.pane == 1 && len(m.data.Acks.Rows) > 0 {
		row := m.data.Acks.Rows[m.ackIx]
		return joinLines(
			kvLine("Policy", row.PolicyID),
			kvLine("Ack Event", row.AckEventID),
			kvLine("Result", row.Result),
			kvLine("Controller", row.ControllerInstance),
			kvLine("Scope", row.ScopeIdentifier),
			kvLine("Observed", formatTimestampAuto(row.ObservedAt)),
			kvLine("Workflow", row.WorkflowID),
		)
	}
	if len(m.data.Outbox.Rows) > 0 {
		row := m.data.Outbox.Rows[m.outboxIx]
		return joinLines(
			kvLine("Outbox", row.ID),
			kvLine("Policy", row.PolicyID),
			kvLine("Status", row.Status),
			kvLine("Source", row.SourceID),
			kvLine("Type", row.SourceType),
			kvLine("Workflow", row.WorkflowID),
			kvLine("Created", formatTimestampAuto(row.CreatedAt)),
		)
	}
	return "-"
}

func renderMonitorOutboxList(rows []outboxRow, selected int, active bool, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render("Outbox")}
	if len(rows) == 0 {
		return append(lines, "- no rows -")
	}
	start, end := listWindow(len(rows), selected, maxRows)
	if start > 0 {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d earlier rows ...", start)))
	}
	for i := start; i < end; i++ {
		row := rows[i]
		prefix := " "
		if active && i == selected {
			prefix = cursorStyle.String()
		}
		if compact {
			lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, shortID(row.PolicyID, 14), renderStatus(row.Status)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s", prefix, shortID(row.PolicyID, 18), renderStatus(row.Status), blankDash(shortID(row.WorkflowID, 18))))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}

func renderMonitorAckList(rows []ackRow, selected int, active bool, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render("ACK History")}
	if len(rows) == 0 {
		return append(lines, "- no rows -")
	}
	start, end := listWindow(len(rows), selected, maxRows)
	if start > 0 {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d earlier rows ...", start)))
	}
	for i := start; i < end; i++ {
		row := rows[i]
		prefix := " "
		if active && i == selected {
			prefix = cursorStyle.String()
		}
		if compact {
			lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, shortID(row.PolicyID, 14), renderStatus(row.Result)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s", prefix, shortID(row.PolicyID, 18), renderStatus(row.Result), blankDash(shortID(row.WorkflowID, 18))))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}
