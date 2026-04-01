package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type ackModel struct {
	ctx          context.Context
	fetch        ackFetcher
	title        string
	autoExit     bool
	loading      bool
	data         ackListResponse
	selected     int
	err          error
	showHelp     bool
	scrollOffset int
	width        int
	height       int
}

func newAckModel(ctx context.Context, title string, fetch ackFetcher) tea.Model {
	width, height := initialTerminalSize()
	return &ackModel{
		ctx:      ctx,
		fetch:    fetch,
		title:    title,
		autoExit: tuiAutoExitEnabled(),
		loading:  true,
		width:    width,
		height:   height,
	}
}

func (m *ackModel) Init() tea.Cmd { return asyncAckLoad(m.ctx, m.fetch) }

func (m *ackModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			return m, asyncAckLoad(m.ctx, m.fetch)
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.scrollOffset = 0
			}
		case "down", "j":
			if m.selected < len(m.data.Rows)-1 {
				m.selected++
				m.scrollOffset = 0
			}
		}
	case ackLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.data = msg.data
			if m.selected >= len(m.data.Rows) {
				m.selected = maxInt(0, len(m.data.Rows)-1)
			}
		}
		if m.autoExit {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *ackModel) selectedRow() *ackRow {
	if len(m.data.Rows) == 0 || m.selected < 0 || m.selected >= len(m.data.Rows) {
		return nil
	}
	return &m.data.Rows[m.selected]
}

func (m *ackModel) View() string {
	compact := compactLayout(m.width)
	header := []string{
		titleStyle.Render(m.title),
		helpStyle.Render(ackHelpText(compact)),
		"",
	}
	if m.loading {
		return joinLines(append(header, "Loading ACK rows...")...)
	}
	if m.showHelp {
		return renderHelpOverlay("ACK Help", []string{
			"j/k or arrows: move selected ACK row",
			"PgUp / PgDn: scroll the full detail body",
			"Ctrl+U / Ctrl+D: faster full-body scroll",
			"r: refresh",
			"q: quit",
		}, m.height)
	}
	if m.err != nil {
		header = append(header, errorStyle.Render(m.err.Error()))
	}
	body := []string{""}
	body = append(body, renderAckPane(m.data.Rows, m.selected, listRowsForHeight(m.height), compact)...)
	body = append(body, "", labelStyle.Render("Selected ACK"))
	if row := m.selectedRow(); row != nil {
		body = append(body,
			kvLine("Policy", row.PolicyID),
			kvLine("Result", renderStatus(row.Result)),
			kvLine("Controller", row.ControllerInstance),
			kvLine("Scope", row.ScopeIdentifier),
			kvLine("ACK Event", row.AckEventID),
			kvLine("Request", row.RequestID),
			kvLine("Command", row.CommandID),
			kvLine("Workflow", row.WorkflowID),
			kvLine("Trace", row.TraceID),
			kvLine("Observed At", formatTimestampAuto(row.ObservedAt)),
		)
		if !compact {
			body = append(body,
				kvLine("Reason", row.Reason),
				kvLine("Error", row.ErrorCode),
				kvLine("QC Reference", row.QCReference),
				kvLine("Source Event", row.SourceEventID),
				kvLine("Sentinel Event", row.SentinelEventID),
			)
		}
	} else {
		body = append(body, "No ACK rows available.")
	}
	return finalizeView(header, body, m.height, m.scrollOffset)
}

func ackHelpText(compact bool) string {
	if compact {
		return "j/k move • ? help • pg scroll • r refresh • q quit"
	}
	return "j/k move • ? help • pgup/pgdn scroll • r refresh • q quit"
}

func renderAckPane(rows []ackRow, selected int, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render("ACK Rows")}
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
		if i == selected {
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
