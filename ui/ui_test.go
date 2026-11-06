package ui

import (
	"math/rand/v2"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kikuchi-null/skyisle-trader/game"
)

var specialKeys = map[string]tea.KeyType{
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"shift+left": tea.KeyShiftLeft, "shift+right": tea.KeyShiftRight,
	"tab": tea.KeyTab, "enter": tea.KeyEnter, "esc": tea.KeyEsc, "backspace": tea.KeyBackspace,
}

func press(m Model, key string) Model {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	if k, ok := specialKeys[key]; ok {
		msg = tea.KeyMsg{Type: k}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func newModel(seed int64) Model {
	next, _ := New(game.New(seed)).Update(tea.WindowSizeMsg{Width: minWidth, Height: minHeight})
	return next.(Model)
}

func TestInvestConfirm(t *testing.T) {
	m := newModel(1)
	g := m.g
	port := &g.Islands[g.Here].Port

	m = press(m, "i")
	if m.mode != modeConfirm || !strings.Contains(m.viewPanel(), "投資しますか") {
		t.Fatalf("i で確認に入らない: mode %d", m.mode)
	}
	m = press(m, "n")
	if m.mode != modeNormal || *port != 0 || g.Money != game.StartMoney {
		t.Errorf("n で取り消せない: 段階 %d 所持金 %d", *port, g.Money)
	}

	cost := g.InvestCost()
	m = press(press(m, "i"), "y")
	if m.mode != modeNormal || *port != 1 || g.Money != game.StartMoney-cost {
		t.Errorf("y で投資されない: 段階 %d 所持金 %d", *port, g.Money)
	}
	if m.msgErr || m.msg != game.IslandDefs[g.Here].Name+"の港を 1 に整備 (500,000G)" {
		t.Errorf("メッセージ %q", m.msg)
	}

	g.Money = g.InvestCost()
	m = press(press(m, "i"), "Y")
	if *port != 2 {
		t.Errorf("Y で投資されない: 段階 %d", *port)
	}
}

func TestInvestScreen(t *testing.T) {
	m := newModel(1)
	g := m.g
	name := game.IslandDefs[g.Here].Name
	g.Money = 3_000_000
	g.Islands[g.Here].Port = 1

	if got := strings.Join(m.marketLines(), "\n"); !strings.Contains(got, "港 1/2 値動き 0.5%") {
		t.Errorf("相場の見出し: %q", got)
	}
	if got := strings.Join(m.footerLines(), "\n"); !strings.Contains(got, "[i]") {
		t.Errorf("キーの説明: %q", got)
	}
	want := []string{name + "の港 1 → 2  値動き 0.5 → 0.3%", "費用 1,000,000G  残金 2,000,000G"}
	if got := press(m, "i").footerLines(); got[0] != want[0] || got[1] != want[1] {
		t.Errorf("確認 = %q, want %q", got[:2], want)
	}

	// 出航先の一覧の最後の列が、その島の港の段階
	other := (int(g.Here) + 1) % len(g.Islands)
	g.Islands[other].Port = 2
	row := press(m, "t").destLines()[2+other]
	if !strings.HasSuffix(row, " 2") {
		t.Errorf("出航先の行: %q", row)
	}

	g.Day = game.LastDay
	if got := strings.Join(m.footerLines(), "\n"); !strings.Contains(got, "[i]") {
		t.Errorf("最終日のキーの説明: %q", got)
	}
}

func TestInvestModes(t *testing.T) {
	// 最終日でも投資できる
	m := newModel(1)
	g := m.g
	g.Day = game.LastDay
	if m = press(press(m, "i"), "y"); g.Islands[g.Here].Port != 1 {
		t.Errorf("最終日の投資: 段階 %d", g.Islands[g.Here].Port)
	}

	// 航行中の i は到着まで飛ばすだけ
	m = newModel(1)
	g = m.g
	m = press(press(m, "t"), "enter")
	if m.mode != modeVoyage {
		t.Fatalf("航行に入らない: mode %d", m.mode)
	}
	if m = press(m, "i"); m.mode != modeNormal || g.Money != game.StartMoney {
		t.Errorf("航行中の i: mode %d, 所持金 %d", m.mode, g.Money)
	}
}

func TestInvestRejected(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*game.Game)
		want  error
	}{
		{"所持金不足", func(g *game.Game) { g.Money = 0 }, game.ErrNoMoney},
		{"最大段階", func(g *game.Game) { g.Islands[g.Here].Port = len(game.PortImpacts) - 1 }, game.ErrMaxPort},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newModel(1)
			c.setup(m.g)
			m = press(m, "i")
			if m.mode != modeNormal || !m.msgErr || m.msg != c.want.Error() {
				t.Errorf("mode %d, msg %q", m.mode, m.msg)
			}
		})
	}
}

// TestLineWidths は、大きな金額でも切り捨てられないはずの行が、切る前からパネル幅に収まることを確かめる。
// viewPanel が幅で切るので、描画後の結果では検出できない。積荷の売り先だけは切ってよい。
func TestLineWidths(t *testing.T) {
	m := newModel(1)
	g := m.g
	g.Money = 99_999_999
	g.Day = 50
	g.Islands[g.Here].Port = 1
	good := m.good
	g.Cargo[good] = 1

	check := func(name string, lines []string) {
		t.Helper()
		for _, l := range lines {
			if w := lipgloss.Width(l); w > panelWidth {
				t.Errorf("%s: 幅 %d: %q", name, w, l)
			}
		}
	}
	check("ヘッダ", m.headerLines())
	check("相場", m.marketLines())
	check("通常のフッタ", m.footerLines())
	for _, key := range []string{"i", "u", "q"} {
		check("確認 "+key, press(m, key).footerLines())
	}
	check("売買", press(m, "b").tradeLines())
	check("売却", press(m, "s").tradeLines())
	check("出航先", press(m, "t").destLines())

	g.Day = game.LastDay
	check("最終日のフッタ", m.footerLines())
}

func TestRandomKeys(t *testing.T) {
	keys := []string{
		"up", "down", "left", "right", "shift+left", "shift+right", "tab", "enter", "esc", "backspace",
		"b", "s", "t", "w", "u", "i", "m", "y", "n", "q", "0", "5", "9",
	}
	r := rand.New(rand.NewPCG(1, 0))
	m := newModel(1)
	for i := range 3000 {
		if m.mode == modeEnd || r.IntN(100) == 0 {
			// 終盤や金額の大きい状態も踏むよう、日付と所持金を振る
			m = newModel(int64(i))
			m.g.Day, m.g.Money = 1+r.IntN(game.LastDay), r.IntN(50_000_000)
		}
		m = press(m, keys[r.IntN(len(keys))])
		if m.mode == modeVoyage && r.IntN(2) == 0 {
			next, _ := m.Update(tickMsg{m.voyage})
			m = next.(Model)
		}
		if w, h := lipgloss.Size(m.View()); w > minWidth || h > minHeight {
			t.Fatalf("op %d mode %d: 画面が %d×%d", i, m.mode, w, h)
		}
		if m.mode != modeEnd {
			if n := strings.Count(m.viewPanel(), "\n") + 1; n > boxHeight {
				t.Fatalf("op %d mode %d: パネルが %d 行", i, m.mode, n)
			}
		}
	}
}
