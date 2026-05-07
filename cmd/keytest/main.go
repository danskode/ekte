package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
)

type model struct{}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		fmt.Printf("Type: %-20v String: %-20q Runes: %v Alt: %v\n",
			msg.Type, msg.String(), msg.Runes, msg.Alt)
	}
	return m, nil
}

func (m model) View() string {
	return "Tryk på taster for at se hvad de sender. Ctrl+C afslutter.\n"
}

func main() {
	p := tea.NewProgram(model{})
	p.Run()
}
