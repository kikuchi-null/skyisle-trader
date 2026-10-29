package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kikuchi-null/skyisle-trader/game"
	"github.com/kikuchi-null/skyisle-trader/ui"
)

func main() {
	seed := flag.Int64("seed", time.Now().UnixNano(), "マップと相場を決めるシード。同じ値で同じ条件を遊び直せる")
	flag.Parse()

	p := tea.NewProgram(ui.New(game.New(*seed)), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
