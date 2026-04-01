package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type validatorsModel struct {
	ctx          context.Context
	fetch        validatorsFetcher
	autoExit     bool
	loading      bool
	data         validatorListResponse
	selected     int
	err          error
	showHelp     bool
	scrollOffset int
	width        int
	height       int
}

func newValidatorsModel(ctx context.Context, fetch validatorsFetcher) tea.Model {
	width, height := initialTerminalSize()
	return &validatorsModel{
		ctx:      ctx,
		fetch:    fetch,
		autoExit: tuiAutoExitEnabled(),
		loading:  true,
		width:    width,
		height:   height,
	}
}

func (m *validatorsModel) Init() tea.Cmd { return asyncValidatorsLoad(m.ctx, m.fetch) }

func (m *validatorsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			return m, asyncValidatorsLoad(m.ctx, m.fetch)
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.scrollOffset = 0
			}
		case "down", "j":
			if m.selected < len(m.data.Validators)-1 {
				m.selected++
				m.scrollOffset = 0
			}
		}
	case validatorsLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.data = msg.data
			if m.selected >= len(m.data.Validators) {
				m.selected = maxInt(0, len(m.data.Validators)-1)
			}
		}
		if m.autoExit {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *validatorsModel) selectedRow() *struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
	Status    string `json:"status"`
} {
	if len(m.data.Validators) == 0 || m.selected < 0 || m.selected >= len(m.data.Validators) {
		return nil
	}
	return &m.data.Validators[m.selected]
}

func (m *validatorsModel) View() string {
	compact := compactLayout(m.width)
	header := []string{
		titleStyle.Render("CyberMesh Validators"),
		helpStyle.Render(validatorsHelpText(compact)),
		"",
		kvLine("Total", fmt.Sprintf("%d", m.data.Total)),
		kvLine("Active", fmt.Sprintf("%d", countValidatorsByStatus(m.data.Validators, "active"))),
		kvLine("Inactive", fmt.Sprintf("%d", countValidatorsByStatus(m.data.Validators, "inactive"))),
	}
	if m.loading {
		return joinLines(append(header, "Loading validators...")...)
	}
	if m.showHelp {
		return renderHelpOverlay("Validators Help", []string{
			"j/k or arrows: move selected validator",
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
	body = append(body, renderValidatorsPane(m.data.Validators, m.selected, listRowsForHeight(m.height), compact)...)
	body = append(body, "", labelStyle.Render("Selected Validator"))
	if row := m.selectedRow(); row != nil {
		body = append(body,
			kvLine("ID", row.ID),
			kvLine("Status", renderStatus(row.Status)),
			kvLine("Public Key", row.PublicKey),
		)
	} else {
		body = append(body, "No validators available.")
	}
	return finalizeView(header, body, m.height, m.scrollOffset)
}

func validatorsHelpText(compact bool) string {
	if compact {
		return "j/k move • ? help • pg scroll • r refresh • q quit"
	}
	return "j/k move • ? help • pgup/pgdn scroll • r refresh • q quit"
}

func renderValidatorsPane(rows []struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
	Status    string `json:"status"`
}, selected int, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render("Validators")}
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
			lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, shortID(row.ID, 14), renderStatus(row.Status)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, shortID(row.ID, 22), renderStatus(row.Status)))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}
