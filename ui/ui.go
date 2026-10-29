// Package ui は skyisle-trader の端末 UI。
// 状態は game.Game から読むだけで、変更はすべて game のメソッドを通す。
package ui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kikuchi-null/skyisle-trader/game"
)

const (
	minWidth, minHeight = 100, 24
	mapCols, mapRows    = 60, 20 // 地図のマス数。y は 2 単位で 1 マス
	boxHeight           = mapRows + 2
	panelWidth          = minWidth - (mapCols + 2) - 1
	frameDelay          = 200 * time.Millisecond // 航行アニメーションの 1 日あたりの時間
	warnDays            = 10                     // 残り日数がこれ以下なら色を変える
)

var (
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	keyStyle    = lipgloss.NewStyle().Bold(true)
	hereStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	targetStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	shipStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	gainStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	attrStyles  = []lipgloss.Style{
		game.AttrSpecialty: gainStyle,
		game.AttrDemand:    warnStyle,
		game.AttrNormal:    lipgloss.NewStyle(),
		game.AttrNone:      dimStyle,
	}
)

type mode int

const (
	modeNormal mode = iota
	modeTrade
	modeSelectDest
	modeVoyage
	modeConfirm // 終了か拡張の確認。どちらかは confirmUpgrade で区別する
	modeEnd
)

// tickMsg は航行アニメーションを 1 コマ進める。voyage が今の航行と違うものは捨てる。
type tickMsg struct{ voyage int }

type Model struct {
	g             *game.Game
	mode          mode
	width, height int
	msg           string // 情報パネルの下に出す 1 行
	msgErr        bool

	good   game.GoodID // 選んでいる品目。通常モードでも保ち、b / s でそのまま売買を始める
	buying bool        // 売買モード: true なら買う
	qty    int
	typing bool // 個数を数字で入力している途中。次の数字を後ろに足す

	dest   game.IslandID // 出航先選択・航行中の行き先
	from   game.IslandID // 航行中の出航元
	start  game.Vec      // 航行中の出航元の位置
	snaps  []game.Snapshot
	frame  int
	voyage int // 航行の通し番号

	confirmUpgrade bool
}

func New(g *game.Game) Model {
	m := Model{g: g}
	m.good = m.nextGood(game.GoodID(len(game.Goods)-1), 1)
	return m
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		if m.mode == modeVoyage && msg.voyage == m.voyage {
			return m.nextFrame()
		}
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.mode {
		case modeNormal:
			return m.updateNormal(key), nil
		case modeTrade:
			return m.updateTrade(key), nil
		case modeSelectDest:
			return m.updateSelectDest(key)
		case modeVoyage:
			return m.arrive(), nil
		case modeConfirm:
			return m.updateConfirm(key), nil
		case modeEnd:
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Model) report(err error, ok string) {
	if err != nil {
		m.msg, m.msgErr = err.Error(), true
	} else {
		m.msg, m.msgErr = ok, false
	}
}

func (m Model) updateNormal(key string) Model {
	m.report(nil, "")
	switch key {
	case "up", "k":
		m.good = m.nextGood(m.good, -1)
	case "down", "j":
		m.good = m.nextGood(m.good, 1)
	case "b", "s":
		m.mode, m.buying = modeTrade, key == "b"
		m.setQty(1)
	case "t":
		if m.g.Over() {
			m.report(game.ErrTooLate, "")
			break
		}
		m.mode = modeSelectDest
		m.dest = m.bestDest()
	case "w":
		m.report(m.g.Wait(), "1 日待機しました")
	case "u":
		if m.g.UpgradeCost() > m.g.Money {
			m.report(game.ErrNoMoney, "")
			break
		}
		m.mode, m.confirmUpgrade = modeConfirm, true
	case "q":
		m.mode, m.confirmUpgrade = modeConfirm, false
	}
	return m
}

func (m Model) updateConfirm(key string) Model {
	m.mode = modeNormal
	m.report(nil, "")
	if key != "y" && key != "Y" {
		return m
	}
	if !m.confirmUpgrade {
		m.mode = modeEnd
		return m
	}
	cost := m.g.UpgradeCost()
	m.report(m.g.Upgrade(), fmt.Sprintf("積載量を %d に拡張 (%sG)", m.g.Capacity, comma(cost)))
	return m
}

func (m Model) updateTrade(key string) Model {
	typing := m.typing
	m.typing = false
	switch key {
	case "up", "k":
		m.good = m.nextGood(m.good, -1)
		m.setQty(1)
	case "down", "j":
		m.good = m.nextGood(m.good, 1)
		m.setQty(1)
	case "left", "h":
		m.setQty(m.qty - 1)
	case "right", "l":
		m.setQty(m.qty + 1)
	case "shift+left":
		m.setQty(m.qty - 10)
	case "shift+right":
		m.setQty(m.qty + 10)
	case "m":
		m.setQty(m.maxQty())
	case "backspace":
		m.setQty(m.qty / 10)
		m.typing = true
	case "tab", "b", "s":
		m.buying = key == "b" || key == "tab" && !m.buying
		m.setQty(m.qty)
	case "enter":
		m.trade()
	case "esc":
		m.mode = modeNormal
		m.report(nil, "")
	default:
		// 数字: 1 文字目で置き換え、続けて押すと後ろに足す
		if len(key) == 1 && key[0]-'0' <= 9 {
			q := int(key[0] - '0')
			if typing {
				q += m.qty * 10
			}
			m.setQty(q)
			m.typing = true
		}
	}
	return m
}

// trade は選んでいる品目と個数で売買を確定する。
func (m *Model) trade() {
	g, name := m.g, game.Goods[m.good].Name
	total, _, err := g.Quote(m.good, m.qty, m.buying)
	ok := fmt.Sprintf("購入 %s×%d %sG", name, m.qty, comma(total))
	switch {
	case err != nil:
	case m.buying:
		err = g.Buy(m.good, m.qty)
	default:
		ok = fmt.Sprintf("売却 %s×%d 損益 %sG", name, m.qty, signed(total-g.CostOf(m.good, m.qty)))
		err = g.Sell(m.good, m.qty)
	}
	m.report(err, ok)
	m.setQty(m.qty)
}

func (m Model) maxQty() int { return m.g.MaxTrade(m.good, m.buying) }

// setQty は個数を 1〜最大数に収める。最大数が 0 のときは 1 にして、確定時に理由をエラーで見せる。
func (m *Model) setQty(q int) { m.qty = min(max(q, 1), max(m.maxQty(), 1)) }

func (m Model) market(good game.GoodID) *game.Market {
	return &m.g.Islands[m.g.Here].Market[good]
}

// nextGood は good から dir 方向に、現在地で取り扱っている次の品目を返す。
func (m Model) nextGood(good game.GoodID, dir int) game.GoodID {
	return game.GoodID(cycle(int(good), dir, len(game.Goods), func(i int) bool {
		return m.market(game.GoodID(i)).Traded()
	}))
}

// nextDest は id から dir 方向に、現在地を飛ばして次の島を返す。
func (m Model) nextDest(id game.IslandID, dir int) game.IslandID {
	return game.IslandID(cycle(int(id), dir, len(m.g.Islands), func(i int) bool {
		return game.IslandID(i) != m.g.Here
	}))
}

// cycle は i から dir 方向に 1 つずつ進み、ok を満たす最初の添字を返す。n 個で一周する。
func cycle(i, dir, n int, ok func(int) bool) int {
	for range n {
		if i = (i + dir + n) % n; ok(i) {
			return i
		}
	}
	return i
}

// bestDest は出航先のカーソルの初期位置。最終日までに着ける島のうち、1 日あたりの目安の損益が最大の島。
// 積荷がなければ損益はどこも 0 なので、一番近い島になる。
func (m Model) bestDest() game.IslandID {
	g := m.g
	best, bestLate, bestProfit, bestDays := game.IslandID(-1), false, 0, 0
	// 比べる順: 最終日までに着けるか → 1 日あたりの損益(割り算を避けて掛け算で比べる) → 近さ
	better := func(late bool, profit, days int) bool {
		if late != bestLate {
			return !late
		}
		if a, b := profit*bestDays, bestProfit*days; a != b {
			return a > b
		}
		return days < bestDays
	}
	for i := range g.Islands {
		id := game.IslandID(i)
		if id == g.Here {
			continue
		}
		days := g.TravelDays(id)
		sales, cost := g.Estimate(id)
		if late, profit := g.Day+days > game.LastDay, sales-cost; best < 0 || better(late, profit, days) {
			best, bestLate, bestProfit, bestDays = id, late, profit, days
		}
	}
	return best
}

func (m Model) updateSelectDest(key string) (Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.dest = m.nextDest(m.dest, -1)
	case "down", "j":
		m.dest = m.nextDest(m.dest, 1)
	case "enter":
		from, start := m.g.Here, m.g.Islands[m.g.Here].Pos
		snaps, err := m.g.Sail(m.dest)
		if err != nil {
			m.report(err, "")
			return m, nil
		}
		m.voyage++
		m.mode, m.from, m.start, m.snaps, m.frame = modeVoyage, from, start, snaps, 0
		m.report(nil, "")
		return m, tick(m.voyage)
	case "esc":
		m.mode = modeNormal
		m.report(nil, "")
	}
	return m, nil
}

func tick(voyage int) tea.Cmd {
	return tea.Tick(frameDelay, func(time.Time) tea.Msg { return tickMsg{voyage} })
}

func (m Model) nextFrame() (Model, tea.Cmd) {
	if m.frame++; m.frame < len(m.snaps) {
		return m, tick(m.voyage)
	}
	return m.arrive(), nil
}

// arrive は航行を終える。アニメーションを飛ばしたときもここに来る。
func (m Model) arrive() Model {
	m.mode, m.snaps = modeNormal, nil
	if !m.market(m.good).Traded() {
		m.good = m.nextGood(m.good, 1)
	}
	msg := game.IslandDefs[m.dest].Name + "に到着"
	var demand []string
	for j, n := range m.g.Cargo {
		if n > 0 && m.market(game.GoodID(j)).Attr == game.AttrDemand {
			demand = append(demand, game.Goods[j].Name)
		}
	}
	if len(demand) > 0 {
		msg += " ― " + strings.Join(demand, "・") + "に需要あり"
	}
	m.report(nil, msg)
	return m
}

func (m Model) View() string {
	if m.width < minWidth || m.height < minHeight {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			fmt.Sprintf("端末を %d×%d 以上に広げてください (現在 %d×%d)", minWidth, minHeight, m.width, m.height))
	}
	body := m.viewEnd()
	if m.mode != modeEnd {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.viewMap(), " ", m.viewPanel())
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) viewEnd() string {
	g := m.g
	// 行ごとに中央寄せされないよう、左揃えのブロックにしてから全体を中央に置く
	return lipgloss.NewStyle().Align(lipgloss.Left).Render(strings.Join([]string{
		"ゲーム終了",
		"",
		"スコア  " + signed(g.Score()) + "G",
		"所持金  " + comma(g.Money) + "G",
		fmt.Sprintf("積載量  %d (拡張 %d 回)", g.Capacity, g.Upgrades),
		fmt.Sprintf("seed    %d", g.Seed),
		"",
		dimStyle.Render(fmt.Sprintf("同じ条件で遊ぶ: skyisle-trader -seed %d", g.Seed)),
		dimStyle.Render("何かキーを押すと終了します"),
	}, "\n"))
}

func (m Model) viewMap() string {
	voyaging := m.mode == modeVoyage
	var islands []game.Vec
	if voyaging {
		islands = m.snaps[m.frame].Islands
	} else {
		for _, is := range m.g.Islands {
			islands = append(islands, is.Pos)
		}
	}
	var c canvas
	c.clear()
	if from, to, ok := m.path(); ok {
		c.line(from, to, dimStyle)
		c.put(to, "◇", targetStyle)
	}
	for i, p := range islands {
		id := game.IslandID(i)
		mark, st := "◆", lipgloss.NewStyle()
		switch {
		case !voyaging && id == m.g.Here:
			mark, st = "●", hereStyle
		case (voyaging || m.mode == modeSelectDest) && id == m.dest:
			st = targetStyle
		case m.sellable(id):
			st = warnStyle
		}
		c.put(p, mark+game.IslandDefs[i].Mark, st)
	}
	if voyaging {
		c.put(m.snaps[m.frame].Ship, "▲", shipStyle)
	}

	title := " 空域 "
	legend := " " + strings.Join([]string{
		hereStyle.Render("●") + dimStyle.Render("現在地"),
		warnStyle.Render("◆") + dimStyle.Render("売り先"),
		targetStyle.Render("◇") + dimStyle.Render("合流点"),
		shipStyle.Render("▲") + dimStyle.Render("船"),
	}, " ") + " "
	lines := []string{"┌" + title + strings.Repeat("─", mapCols-lipgloss.Width(title)) + "┐"}
	for _, row := range c.rows() {
		lines = append(lines, "│"+row+"│")
	}
	lines = append(lines, "└─"+legend+strings.Repeat("─", mapCols-1-lipgloss.Width(legend))+"┘")
	return strings.Join(lines, "\n")
}

// path は地図に描く航路の両端(出航元と合流点)。出航先選択と航行中だけ描く。
func (m Model) path() (from, to game.Vec, ok bool) {
	switch m.mode {
	case modeSelectDest:
		_, to = m.g.Route(m.dest)
		return m.g.Islands[m.g.Here].Pos, to, true
	case modeVoyage:
		return m.start, m.snaps[len(m.snaps)-1].Ship, true
	}
	return from, to, false
}

// sellable は、現在地以外で、持っている品目に需要がある島かを返す。
func (m Model) sellable(id game.IslandID) bool {
	if id == m.g.Here {
		return false
	}
	for j, n := range m.g.Cargo {
		if n > 0 && m.g.Islands[id].Market[j].Attr == game.AttrDemand {
			return true
		}
	}
	return false
}

func (m Model) viewPanel() string {
	lines := m.headerLines()
	switch m.mode {
	case modeSelectDest:
		lines = append(lines, m.destLines()...)
	case modeVoyage:
		lines = append(lines, m.voyageLines()...)
	case modeTrade:
		lines = append(append(lines, m.marketLines()...), m.tradeLines()...)
	default:
		lines = append(append(lines, m.marketLines()...), m.cargoLines()...)
	}

	footer := m.footerLines()
	for len(lines) < boxHeight-len(footer) {
		lines = append(lines, "")
	}
	lines = append(lines, footer...)
	clip := lipgloss.NewStyle().MaxWidth(panelWidth)
	for i, l := range lines {
		lines[i] = clip.Render(l)
	}
	return strings.Join(lines, "\n")
}

func (m Model) headerLines() []string {
	g := m.g
	day, place := g.Day, "現在地 "+game.IslandDefs[g.Here].Name
	if m.mode == modeVoyage {
		day, place = m.snaps[m.frame].Day, "航行中 → "+game.IslandDefs[m.dest].Name
	}
	dayLine := fmt.Sprintf("Day %d/%d  ", day, game.LastDay)
	switch left := game.LastDay - day; {
	case left == 0:
		dayLine = errStyle.Render(dayLine + "最終日")
	case left <= warnDays:
		dayLine = warnStyle.Render(fmt.Sprintf("%s残り %d 日", dayLine, left))
	default:
		dayLine += fmt.Sprintf("残り %d 日", left)
	}
	return []string{
		dayLine,
		"所持金 " + comma(g.Money) + "G  スコア " + signed(g.Score()),
		fmt.Sprintf("%s  積荷 %d/%d", place, g.Load(), g.Capacity),
		"",
	}
}

func (m Model) marketLines() []string {
	g := m.g
	lines := []string{
		rule("相場 " + game.IslandDefs[g.Here].Name),
		dimStyle.Render("  " + padR("品目", 6) + " " + padL("買値", 7) + " " + padL("売値", 7) + " 属性 " + padL("積荷", 4)),
	}
	for j, gd := range game.Goods {
		mk := m.market(game.GoodID(j))
		var row string
		if mk.Traded() {
			row = padR(gd.Name, 6) + " " + padL(comma(mk.Price), 7) + " " + padL(comma(mk.SellPrice()), 7) + " " +
				attrStyles[mk.Attr].Render(game.Attrs[mk.Attr].Name)
		} else {
			row = dimStyle.Render(padR(gd.Name, 6) + " " + padR(game.Attrs[mk.Attr].Name, 20))
		}
		lines = append(lines, m.cursor(game.GoodID(j) == m.good)+row+" "+padL(strconv.Itoa(g.Cargo[j]), 4))
	}
	return append(lines, "")
}

// cargoLines は、持っている品目ごとに需要がある島を近い順に並べる。最終日までに着けない島はグレー。
func (m Model) cargoLines() []string {
	g := m.g
	lines := []string{rule("積荷の売り先")}
	if g.Load() == 0 {
		return append(lines, dimStyle.Render("積荷はありません"))
	}
	for j, n := range g.Cargo {
		if n == 0 {
			continue
		}
		row := []string{padR(fmt.Sprintf("%s×%d", game.Goods[j].Name, n), 9)}
		if m.market(game.GoodID(j)).Attr == game.AttrDemand {
			row = append(row, warnStyle.Render("ここ"))
		}
		var dests []game.IslandID
		for i, is := range g.Islands {
			if id := game.IslandID(i); id != g.Here && is.Market[j].Attr == game.AttrDemand {
				dests = append(dests, id)
			}
		}
		slices.SortFunc(dests, func(a, b game.IslandID) int { return g.TravelDays(a) - g.TravelDays(b) })
		for _, id := range dests {
			d := g.TravelDays(id)
			s := fmt.Sprintf("%s%d日", game.IslandDefs[id].Name, d)
			if g.Day+d > game.LastDay {
				s = dimStyle.Render(s)
			}
			row = append(row, s)
		}
		lines = append(lines, strings.Join(row, " "))
	}
	return lines
}

// tradeLines は売買を確定する前に、合計額と、確定した後の相場・所持金・積荷(売るときは損益も)を見せる。
func (m Model) tradeLines() []string {
	g := m.g
	verb, sign := "売る", 1 // sign は所持金が増える向き。積荷はその逆に動く
	if m.buying {
		verb, sign = "買う", -1
	}
	lines := []string{
		rule(verb + " " + game.Goods[m.good].Name),
		fmt.Sprintf("個数   %d (最大 %d)", m.qty, m.maxQty()),
	}
	total, price, err := g.Quote(m.good, m.qty, m.buying)
	if total > 0 {
		load := g.Load() - sign*m.qty*game.Goods[m.good].Weight
		lines = append(lines,
			fmt.Sprintf("合計   %sG  平均 %sG", comma(total), comma(total/m.qty)),
			fmt.Sprintf("相場   %s → %sG", comma(m.market(m.good).Price), comma(price)),
			fmt.Sprintf("所持金 %s → %sG", comma(g.Money), comma(g.Money+sign*total)),
			fmt.Sprintf("積荷   %d → %d/%d", g.Load(), load, g.Capacity))
		if !m.buying {
			cost := g.CostOf(m.good, m.qty)
			lines = append(lines, fmt.Sprintf("損益   %sG  仕入れ平均 %sG", signed(total-cost), comma(cost/m.qty)))
		}
	}
	if err != nil {
		lines = append(lines, errStyle.Render(err.Error()))
	}
	return lines
}

func (m Model) destLines() []string {
	g := m.g
	head := dimStyle.Render("  " + padR("島", 6) + " " + padL("日数", 4) + " " + padL("到着", 4) + " ")
	for j, gd := range game.Goods {
		if g.Cargo[j] > 0 {
			head += " " + keyStyle.Render(gd.Short) // 持っている品目の列は明るくする
		} else {
			head += " " + dimStyle.Render(gd.Short)
		}
	}
	lines := []string{rule("出航先"), head}
	for i, def := range game.IslandDefs {
		id := game.IslandID(i)
		days, arrive, gray := "現在", "", id == g.Here
		if !gray {
			d := g.TravelDays(id)
			days, arrive, gray = strconv.Itoa(d)+"日", "D"+strconv.Itoa(g.Day+d), g.Day+d > game.LastDay
		}
		row := padR(def.Name, 6) + " " + padL(days, 4) + " " + padL(arrive, 4) + " "
		for _, mk := range g.Islands[i].Market {
			st := attrStyles[mk.Attr]
			if gray {
				st = dimStyle
			}
			row += " " + st.Render(game.Attrs[mk.Attr].Short)
		}
		if gray {
			row = dimStyle.Render(row)
		}
		lines = append(lines, m.cursor(id == m.dest)+row)
	}

	d := g.TravelDays(m.dest)
	lines = append(lines, "", rule(fmt.Sprintf("%s %d日 (Day %d 着)", game.IslandDefs[m.dest].Name, d, g.Day+d)))
	sales, cost := g.Estimate(m.dest)
	switch {
	case g.Day+d > game.LastDay:
		lines = append(lines, errStyle.Render(game.ErrTooLate.Error()))
	case g.Load() == 0:
		lines = append(lines, dimStyle.Render("積荷はありません"))
	case sales == 0:
		lines = append(lines, warnStyle.Render("積荷はこの島では売れません"))
	default:
		lines = append(lines, fmt.Sprintf("売却目安 %sG  損益 %sG", comma(sales), signed(sales-cost)))
	}
	return lines
}

func (m Model) voyageLines() []string {
	const barWidth = panelWidth - 4
	done := barWidth * (m.frame + 1) / len(m.snaps)
	return []string{
		rule("航行"),
		game.IslandDefs[m.from].Name + " → " + game.IslandDefs[m.dest].Name,
		fmt.Sprintf("Day %d / 到着 Day %d", m.snaps[m.frame].Day, m.g.Day),
		shipStyle.Render(strings.Repeat("━", done)) + dimStyle.Render(strings.Repeat("─", barWidth-done)),
	}
}

func (m Model) cursor(on bool) string {
	switch {
	case !on:
		return "  "
	case m.mode == modeTrade:
		return hereStyle.Render("> ")
	}
	return dimStyle.Render("> ")
}

func (m Model) footerLines() []string {
	g := m.g
	if m.mode == modeConfirm {
		if m.confirmUpgrade {
			cost := g.UpgradeCost()
			return []string{
				fmt.Sprintf("積載量 %d → %d  費用 %sG", g.Capacity, g.Capacity+game.UpgradeStep, comma(cost)),
				fmt.Sprintf("所持金 %s → %sG", comma(g.Money), comma(g.Money-cost)),
				hereStyle.Render("拡張しますか? (y/n)"),
			}
		}
		warn := ""
		if g.Load() > 0 {
			warn = warnStyle.Render("積荷はスコアに含まれません")
		}
		return []string{"スコア " + signed(g.Score()) + "G で終了します", warn, hereStyle.Render("終了しますか? (y/n)")}
	}

	msg := m.msg
	if m.msgErr {
		msg = errStyle.Render(msg)
	}
	var help []string
	switch m.mode {
	case modeNormal:
		help = []string{
			hints("↑↓", "選択", "[b]", "買う", "[s]", "売る", "[t]", "出航"),
			hints("[w]", "待機", "[u]", "拡張", "[q]", "終了"),
		}
		if g.Over() {
			help = []string{
				hints("↑↓", "選択", "[b]", "買う", "[s]", "売る", "[u]", "拡張"),
				hints("[q]", "終了してスコアを確定"),
			}
		}
	case modeTrade:
		help = []string{
			hints("↑↓", "品目", "←→", "個数", "[0-9]", "入力", "[m]", "最大"),
			hints("[Tab]", "売買切替", "[Enter]", "確定", "[Esc]", "戻る"),
		}
	case modeSelectDest:
		help = []string{hints("↑↓", "選択", "[Enter]", "出航", "[Esc]", "戻る"), ""}
	case modeVoyage:
		help = []string{hints("何かキー", "で到着まで飛ばす"), ""}
	}
	return append([]string{msg}, help...)
}

// hints はキーと説明の組を 1 行に並べる。キーを明るく、説明をグレーで描く。
func hints(pairs ...string) string {
	var s []string
	for i := 0; i < len(pairs); i += 2 {
		s = append(s, keyStyle.Render(pairs[i])+dimStyle.Render(pairs[i+1]))
	}
	return strings.Join(s, " ")
}

// cell は地図の 1 マス。w == 0 は直前の全角文字の右半分。
type cell struct {
	s string
	w int
}

type canvas [mapRows][mapCols]cell

var blank = cell{" ", 1}

func (c *canvas) clear() {
	for r := range c {
		for k := range c[r] {
			c[r][k] = blank
		}
	}
}

// put は p のマスを起点に text を描く。右端に収まらないときは左へずらす。
// 既存の全角文字に一部だけ重なった場合は、残った片割れを空白にする。
func (c *canvas) put(p game.Vec, text string, st lipgloss.Style) {
	line := &c[min(int(p.Y/2), mapRows-1)]
	col := min(int(p.X), mapCols-lipgloss.Width(text))
	for _, r := range text {
		ch := string(r)
		w := lipgloss.Width(ch)
		if line[col].w == 0 && col > 0 {
			line[col-1] = blank
		}
		if end := col + w; end < mapCols && line[end].w == 0 {
			line[end] = blank
		}
		line[col] = cell{st.Render(ch), w}
		for k := 1; k < w; k++ {
			line[col+k] = cell{}
		}
		col += w
	}
}

// line は a から b まで、両端を除いて 1 マスごとに点を打つ。
func (c *canvas) line(a, b game.Vec, st lipgloss.Style) {
	n := int(max(math.Abs(b.X-a.X), math.Abs(b.Y-a.Y)/2)) // 長い方の軸のマス数
	for i := 1; i < n; i++ {
		t := float64(i) / float64(n)
		c.put(game.Vec{X: a.X + (b.X-a.X)*t, Y: a.Y + (b.Y-a.Y)*t}, "·", st)
	}
}

func (c *canvas) rows() []string {
	rows := make([]string, mapRows)
	for r := range c {
		var b strings.Builder
		for _, x := range c[r] {
			b.WriteString(x.s)
		}
		rows[r] = b.String()
	}
	return rows
}

func rule(title string) string {
	s := "── " + title + " "
	return s + strings.Repeat("─", max(panelWidth-lipgloss.Width(s), 0))
}

func padR(s string, w int) string { return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) }
func padL(s string, w int) string { return strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) + s }

// comma は 3 桁ごとにカンマを入れる。
func comma(n int) string {
	s, sign := strconv.Itoa(n), ""
	if n < 0 {
		s, sign = s[1:], "-"
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + s
}

// signed は損益を符号つきで、プラスは緑、マイナスは赤で描く。
func signed(n int) string {
	switch {
	case n > 0:
		return gainStyle.Render("+" + comma(n))
	case n < 0:
		return errStyle.Render(comma(n))
	}
	return "0"
}
