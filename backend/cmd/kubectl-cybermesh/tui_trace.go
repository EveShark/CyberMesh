package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type traceModel struct {
	ctx          context.Context
	title        string
	fetch        traceFetcher
	autoExit     bool
	loading      bool
	trace        traceResponse
	err          error
	showHelp     bool
	pane         int
	outboxIx     int
	ackIx        int
	scrollOffset int
	width        int
	height       int
}

func newTraceModel(ctx context.Context, title string, fetch traceFetcher) tea.Model {
	width, height := initialTerminalSize()
	return &traceModel{
		ctx:      ctx,
		title:    title,
		fetch:    fetch,
		autoExit: tuiAutoExitEnabled(),
		loading:  true,
		width:    width,
		height:   height,
	}
}

func (m *traceModel) Init() tea.Cmd {
	return asyncTraceLoad(m.ctx, m.fetch)
}

func (m *traceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			return m, asyncTraceLoad(m.ctx, m.fetch)
		case "tab":
			if len(m.trace.Outbox) > 0 && len(m.trace.Acks) > 0 {
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
			if m.pane == 0 && m.outboxIx < len(m.trace.Outbox)-1 {
				m.outboxIx++
			}
			if m.pane == 1 && m.ackIx < len(m.trace.Acks)-1 {
				m.ackIx++
			}
		}
	case traceLoadedMsg:
		m.loading = false
		m.trace = msg.data
		m.err = msg.err
		if m.outboxIx >= len(m.trace.Outbox) {
			m.outboxIx = maxInt(0, len(m.trace.Outbox)-1)
		}
		if m.ackIx >= len(m.trace.Acks) {
			m.ackIx = maxInt(0, len(m.trace.Acks)-1)
		}
		if m.autoExit {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *traceModel) View() string {
	compact := compactLayout(m.width)
	tiny := tinyLayout(m.width)
	header := []string{
		titleStyle.Render("CyberMesh Trace Viewer"),
		helpStyle.Render(traceHelpText(compact)),
		"",
		kvLine("Lookup", m.title),
		kvLine("Policy", m.trace.PolicyID),
		kvLine("Trace", m.trace.TraceID),
	}
	if !compact {
		header = append(header, kvLine("Source Event", m.trace.SourceEventID))
		header = append(header, kvLine("Sentinel Event", m.trace.SentinelEventID))
	}
	if m.loading {
		return joinLines(append(header, "", "Loading trace data...")...)
	}
	if m.showHelp {
		return renderHelpOverlay("Trace Help", []string{
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
	body = append(body, renderTracePaneList("Outbox", m.trace.Outbox, m.outboxIx, m.pane == 0, listRowsForHeight(m.height), compact)...)
	if !tiny {
		body = append(body, "")
		body = append(body, renderAckPaneList("ACK History", m.trace.Acks, m.ackIx, m.pane == 1, listRowsForHeight(m.height), compact)...)
	}
	body = append(body, "")
	body = append(body, labelStyle.Render("Details"))
	body = append(body, m.selectedTraceDetails())
	return finalizeView(header, body, m.height, m.scrollOffset)
}

func traceHelpText(compact bool) string {
	if compact {
		return "j/k move • ? help • tab pane • pg scroll • q quit"
	}
	return "j/k move • ? help • tab switch pane • pgup/pgdn scroll • r refresh • q quit"
}

func (m *traceModel) selectedTraceDetails() string {
	if m.pane == 1 && len(m.trace.Acks) > 0 {
		row := m.trace.Acks[m.ackIx]
		return joinLines(
			kvLine("Ack Event", row.AckEventID),
			kvLine("Result", row.Result),
			kvLine("Controller", row.ControllerInstance),
			kvLine("Scope", row.ScopeIdentifier),
			kvLine("Observed", formatTimestampAuto(row.ObservedAt)),
			kvLine("Request", row.RequestID),
			kvLine("Command", row.CommandID),
			kvLine("Workflow", row.WorkflowID),
			kvLine("Reason", row.Reason),
		)
	}
	if len(m.trace.Outbox) > 0 {
		row := m.trace.Outbox[m.outboxIx]
		return joinLines(
			kvLine("Outbox", row.ID),
			kvLine("Status", row.Status),
			kvLine("Created", formatTimestampAuto(row.CreatedAt)),
			kvLine("Source", row.SourceID),
			kvLine("Source Type", row.SourceType),
			kvLine("Request", row.RequestID),
			kvLine("Command", row.CommandID),
			kvLine("Workflow", row.WorkflowID),
		)
	}
	if m.trace.Materialized != nil {
		return joinLines(
			kvLine("First ACK", ackSummary(m.trace.Materialized.FirstPolicyAck)),
			kvLine("Latest ACK", ackSummary(m.trace.Materialized.LatestPolicyAck)),
		)
	}
	return "-"
}

func renderTracePaneList(title string, rows []outboxRow, selected int, active bool, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render(title)}
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
			lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, shortID(row.ID, 14), renderStatus(row.Status)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s", prefix, shortID(row.ID, 18), renderStatus(row.Status), blankDash(shortID(row.WorkflowID, 18))))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}

func renderAckPaneList(title string, rows []ackRow, selected int, active bool, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render(title)}
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
			lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, shortID(row.AckEventID, 14), renderStatus(row.Result)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s", prefix, shortID(row.AckEventID, 18), renderStatus(row.Result), blankDash(shortID(row.WorkflowID, 18))))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}

func ackSummary(row *struct {
	ControllerInstance string `json:"controller_instance,omitempty"`
	Result             string `json:"result,omitempty"`
	Reason             string `json:"reason,omitempty"`
	ErrorCode          string `json:"error_code,omitempty"`
	AppliedAtMs        int64  `json:"applied_at_ms,omitempty"`
	AckedAtMs          int64  `json:"acked_at_ms,omitempty"`
}) string {
	if row == nil {
		return "-"
	}
	parts := []string{renderStatus(row.Result)}
	if strings.TrimSpace(row.ControllerInstance) != "" {
		parts = append(parts, "via "+row.ControllerInstance)
	}
	return strings.Join(parts, " ")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
