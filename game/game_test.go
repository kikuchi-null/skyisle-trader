package game

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

const (
	moss  GoodID = iota // 浮遊苔
	_                   // 霧酒
	stone               // 魔石
	_                   // 星砂
	scale               // 竜の鱗
)

// fixture は生成結果に依存しない 2 島の世界を作る。
// 島は動かず dist だけ離れ、全商品が通常の相場になる。現在地は島 0。
func fixture(dist float64) *Game {
	g := New(1)
	g.Islands = []Island{{Pos: Vec{10, 20}}, {Pos: Vec{10 + dist, 20}}}
	for i := range g.Islands {
		g.Islands[i].Market = make([]Market, len(Goods))
		for j := range Goods {
			g.Islands[i].Market[j] = newMarket(GoodID(j), AttrNormal)
		}
	}
	g.Here = 0
	return g
}

// state は状態の比較用の文字列。
func state(g *Game) string {
	return fmt.Sprintf("%d %d %d %v %v %d %d %v", g.Day, g.Money, g.Here, g.Cargo, g.Cost, g.Capacity, g.Upgrades, g.Islands)
}

func inSky(p Vec) bool { return p.X >= 0 && p.X <= mapW && p.Y >= 0 && p.Y <= mapH }

func near(a, b Vec) bool { return a.Dist(b) < 1e-9 }

func TestGenerate(t *testing.T) {
	for seed := range int64(200) {
		g := New(seed)
		if g.Day != 1 || g.Money != StartMoney || g.Capacity != startCapacity || g.Load() != 0 {
			t.Fatalf("seed %d: 初期状態が不正: %s", seed, state(g))
		}
		if len(g.Islands) != len(IslandDefs) || g.Here < 0 || int(g.Here) >= len(g.Islands) {
			t.Fatalf("seed %d: 島の数か現在地が不正", seed)
		}
		for i, a := range g.Islands {
			if !inSky(a.Pos) {
				t.Errorf("seed %d: 島 %d が空域外 %v", seed, i, a.Pos)
			}
			if s := math.Hypot(a.Vel.X, a.Vel.Y); s < minDrift || s > maxDrift {
				t.Errorf("seed %d: 島 %d の速さ %v", seed, i, s)
			}
			for j := range i {
				if d := a.Pos.Dist(g.Islands[j].Pos); d < minIslandGap {
					t.Errorf("seed %d: 島 %d と %d の距離 %v", seed, i, j, d)
				}
			}
		}
		count := func(island, good int, a Attr) int {
			n := 0
			for i := range g.Islands {
				for j := range Goods {
					if (island < 0 || island == i) && (good < 0 || good == j) && g.Islands[i].Market[j].Attr == a {
						n++
					}
				}
			}
			return n
		}
		for i := range g.Islands {
			if count(i, -1, AttrSpecialty) == 0 || count(i, -1, AttrDemand) == 0 {
				t.Errorf("seed %d: 島 %d に名産か需要がない", seed, i)
			}
			if count(i, -1, AttrNone) > 1 {
				t.Errorf("seed %d: 島 %d に取扱なしが 2 つ以上", seed, i)
			}
		}
		for j := range Goods {
			if count(-1, j, AttrSpecialty) == 0 || count(-1, j, AttrDemand) == 0 {
				t.Errorf("seed %d: 商品 %d に名産か需要の島がない", seed, j)
			}
		}
		for i := range g.Islands {
			for j, m := range g.Islands[i].Market {
				if want := newMarket(GoodID(j), m.Attr); m != want {
					t.Errorf("seed %d: 相場の初期値 %+v, want %+v", seed, m, want)
				}
			}
		}
	}
}

func TestDrift(t *testing.T) {
	cases := []struct {
		name           string
		pos, vel       Vec
		wantPos, wantV Vec
	}{
		{"空域内", Vec{10, 10}, Vec{1.5, -0.5}, Vec{11.5, 9.5}, Vec{1.5, -0.5}},
		{"右端で反射", Vec{59.5, 20}, Vec{1.5, 0}, Vec{59, 20}, Vec{-1.5, 0}},
		{"左端で反射", Vec{0.5, 20}, Vec{-1.5, 0}, Vec{1, 20}, Vec{1.5, 0}},
		{"上端で反射", Vec{30, 0.5}, Vec{0, -1.5}, Vec{30, 1}, Vec{0, 1.5}},
		{"下端で反射", Vec{30, 39}, Vec{0, 2}, Vec{30, 39}, Vec{0, -2}},
		{"角で両軸とも反射", Vec{59, 39}, Vec{1.5, 1.5}, Vec{59.5, 39.5}, Vec{-1.5, -1.5}},
		{"ちょうど端", Vec{58, 38}, Vec{2, 2}, Vec{60, 40}, Vec{2, 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := fixture(20)
			g.Islands[1].Pos, g.Islands[1].Vel = c.pos, c.vel
			if err := g.Wait(); err != nil {
				t.Fatal(err)
			}
			if is := g.Islands[1]; !near(is.Pos, c.wantPos) || !near(is.Vel, c.wantV) {
				t.Errorf("pos %v vel %v, want %v %v", is.Pos, is.Vel, c.wantPos, c.wantV)
			}
		})
	}

	g := New(1)
	for range 1000 {
		if err := g.Wait(); err != nil {
			g.Day = 1
		}
		for i, is := range g.Islands {
			if !inSky(is.Pos) {
				t.Fatalf("島 %d が空域外 %v", i, is.Pos)
			}
		}
	}
}

func TestTravelDays(t *testing.T) {
	for _, c := range []struct {
		dist float64
		want int
	}{{3, 1}, {8, 1}, {8.01, 2}, {16, 2}, {20, 3}, {50, 7}} {
		g := fixture(c.dist)
		if got := g.TravelDays(1); got != c.want {
			t.Errorf("距離 %v: TravelDays = %d, want %d", c.dist, got, c.want)
		}
	}

	for seed := range int64(50) {
		for dest := range IslandID(len(IslandDefs)) {
			g := New(seed)
			if dest == g.Here {
				continue
			}
			from := g.Islands[g.Here].Pos
			d, arrival := g.Route(dest)
			if g.TravelDays(dest) != d {
				t.Fatalf("seed %d dest %d: TravelDays と Route の日数が違う", seed, dest)
			}
			snaps, err := g.Sail(dest)
			if err != nil {
				t.Fatalf("seed %d dest %d: %v", seed, dest, err)
			}
			if len(snaps) != d || g.Day != 1+d || g.Here != dest {
				t.Fatalf("seed %d dest %d: TravelDays %d, Snapshot %d 枚, Day %d", seed, dest, d, len(snaps), g.Day)
			}
			for k, s := range snaps {
				if s.Day != 2+k {
					t.Errorf("seed %d dest %d: snaps[%d].Day = %d", seed, dest, k, s.Day)
				}
				// d が最小であること: それより前の日には届かない
				reach := from.Dist(s.Islands[dest]) <= shipSpeed*float64(k+1)
				if reach != (k == d-1) {
					t.Errorf("seed %d dest %d: %d 日目に届くか = %v (移動日数 %d)", seed, dest, k+1, reach, d)
				}
			}
			if last := snaps[d-1]; !near(last.Ship, g.Islands[dest].Pos) || !near(snaps[0].Ship, lerp(from, last.Ship, 1/float64(d))) {
				t.Errorf("seed %d dest %d: 飛行船の位置が補間になっていない", seed, dest)
			}
			if !near(arrival, g.Islands[dest].Pos) {
				t.Errorf("seed %d dest %d: Route の合流点 %v, 到着時の位置 %v", seed, dest, arrival, g.Islands[dest].Pos)
			}
		}
	}
}

func TestPrices(t *testing.T) {
	g := fixture(20)
	m := g.market(stone) // 現在価格 10,000

	if got, price, _ := g.Quote(stone, 2, true); got != 10000+10100 || price != 10201 {
		t.Errorf("Quote(買 2) = %d, 取引後の価格 %d", got, price)
	}
	if err := g.Buy(stone, 2); err != nil {
		t.Fatal(err)
	}
	if g.Money != StartMoney-20100 || m.Price != 10201 || g.Cargo[stone] != 2 {
		t.Errorf("買った後: 所持金 %d 価格 %d 積荷 %d", g.Money, m.Price, g.Cargo[stone])
	}
	if got, _, _ := g.Quote(stone, 1, false); got != 9180 || m.SellPrice() != 9180 {
		t.Errorf("売値 = %d, %d, want 9180", got, m.SellPrice())
	}
	if err := g.Sell(stone, 1); err != nil {
		t.Fatal(err)
	}
	if g.Money != StartMoney-20100+9180 || m.Price != 10098 || g.Cargo[stone] != 1 {
		t.Errorf("売った後: 所持金 %d 価格 %d 積荷 %d", g.Money, m.Price, g.Cargo[stone])
	}

	// Quote と Buy / Sell の金額が一致し、同じ島で買ってすぐ売ると損をする
	for j := range Goods {
		g := fixture(20)
		good := GoodID(j)
		for _, n := range []int{1, 5} {
			before := g.Money
			cost, _, _ := g.Quote(good, n, true)
			if err := g.Buy(good, n); err != nil {
				t.Fatal(err)
			}
			gain, _, _ := g.Quote(good, n, false)
			if err := g.Sell(good, n); err != nil {
				t.Fatal(err)
			}
			if g.Money != before-cost+gain {
				t.Errorf("商品 %d × %d: 所持金 %d, Quote からの計算 %d", j, n, g.Money, before-cost+gain)
			}
			if g.Money >= before {
				t.Errorf("商品 %d × %d: 買ってすぐ売って損をしない (%d → %d)", j, n, before, g.Money)
			}
		}
	}
}

func TestCost(t *testing.T) {
	g := fixture(20) // 魔石は 10,000 から 1 個ごとに 1% 上がる
	g.Buy(stone, 2)  // 10,000 + 10,100
	g.Buy(stone, 1)  // 10,201
	if g.Cost[stone] != 30301 || g.CostOf(stone, 2) != 20200 {
		t.Fatalf("仕入れ額 %d, 2 個分 %d", g.Cost[stone], g.CostOf(stone, 2))
	}
	g.Sell(stone, 2)
	if g.Cost[stone] != 30301-20200 {
		t.Errorf("2 個売った後の仕入れ額 %d", g.Cost[stone])
	}
	g.Sell(stone, 1)
	if g.Cost[stone] != 0 || g.CostOf(moss, 1) != 0 {
		t.Errorf("全部売った後の仕入れ額 %d, 積荷なしの CostOf %d", g.Cost[stone], g.CostOf(moss, 1))
	}
}

func TestEstimate(t *testing.T) {
	g := fixture(20)
	g.Buy(stone, 2)
	g.Buy(moss, 3)
	dest := &g.Islands[1]
	dest.Market[stone] = newMarket(stone, AttrDemand) // 16,000
	dest.Market[stone].Price = 1                      // 現在価格は目安に使わない
	dest.Market[moss].Attr = AttrNone                 // 売れない品目は数えない

	sales, cost := g.Estimate(1)
	if want := 16000*90/100 + 15840*90/100; sales != want || cost != g.Cost[stone] {
		t.Errorf("Estimate = %d, %d, want %d, %d", sales, cost, want, g.Cost[stone])
	}
}

func TestRestore(t *testing.T) {
	for _, c := range []struct{ price, want int }{
		{12000, 11700},
		{9000, 9150},
		{10003, 10003}, // 差が 7G 未満だと切り捨てで動かない
		{9993, 9994},
	} {
		m := Market{AttrNormal, 10000, 10000, c.price}
		if m.restore(); m.Price != c.want {
			t.Errorf("restore(%d) = %d, want %d", c.price, m.Price, c.want)
		}
	}

	// 売って下げた価格は、待つと毎日、揺らいだ後の基準価格へ差の 15% だけ近づく
	g := fixture(20)
	g.Cargo[stone] = 50
	if err := g.Sell(stone, 50); err != nil {
		t.Fatal(err)
	}
	m := g.market(stone)
	crashed := m.Price
	for day := range 10 {
		prev := m.Price
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
		if want := prev + (m.Base-prev)*restorePct/100; m.Price != want {
			t.Errorf("%d 日後: 現在価格 %d, want %d (基準価格 %d)", day+1, m.Price, want, m.Base)
		}
	}
	if m.Price <= crashed {
		t.Errorf("10 日待っても価格が戻らない: %d → %d", crashed, m.Price)
	}
}

func TestFluctuate(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 0))
	m := newMarket(stone, AttrDemand)
	lo, hi, moved := m.Init*70/100, m.Init*130/100, false
	for range 10000 {
		prev := m.Base
		m.fluctuate(rng)
		if m.Base < lo || m.Base > hi {
			t.Fatalf("基準価格 %d が [%d, %d] の外", m.Base, lo, hi)
		}
		if d := m.Base - prev; d*100 > prev*5+100 || d*100 < -prev*5-100 {
			t.Fatalf("1 日の揺らぎが大きすぎる: %d → %d", prev, m.Base)
		}
		moved = moved || m.Base != prev
	}
	if !moved {
		t.Error("基準価格が揺らがない")
	}
}

func TestMaxTrade(t *testing.T) {
	g := fixture(20)
	if got := g.MaxTrade(stone, true); got != startCapacity/Goods[stone].Weight {
		t.Errorf("積載量で決まる最大数 = %d", got)
	}
	g.Money = 25000 // 10,000 + 10,100 まで買える
	if got := g.MaxTrade(stone, true); got != 2 {
		t.Errorf("所持金で決まる最大数 = %d", got)
	}
	if err := g.Buy(stone, 2); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(g.Buy(stone, 1), ErrNoMoney) || g.MaxTrade(stone, true) != 0 {
		t.Error("最大数を買った後にさらに買えてしまう")
	}
	if got := g.MaxTrade(stone, false); got != 2 {
		t.Errorf("売れる最大数 = %d", got)
	}
	g.market(moss).Attr = AttrNone
	if g.MaxTrade(moss, true) != 0 {
		t.Error("取扱なしの最大数が 0 でない")
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name  string
		setup func(g *Game)
		op    func(g *Game) error
		want  error
	}{
		{"所持金不足", func(g *Game) { g.Money = 9999 }, func(g *Game) error { return g.Buy(stone, 1) }, ErrNoMoney},
		{"積載量オーバー", func(g *Game) { g.Cargo[moss] = 189 }, func(g *Game) error { return g.Buy(stone, 3) }, ErrOverCapacity},
		{"取扱なしの購入", func(g *Game) { g.market(stone).Attr = AttrNone }, func(g *Game) error { return g.Buy(stone, 1) }, ErrNotTraded},
		{"取扱なしの売却", func(g *Game) { g.market(stone).Attr = AttrNone; g.Cargo[stone] = 1 }, func(g *Game) error { return g.Sell(stone, 1) }, ErrNotTraded},
		{"取扱なしの見積もり", func(g *Game) { g.market(stone).Attr = AttrNone }, func(g *Game) error { _, _, err := g.Quote(stone, 1, true); return err }, ErrNotTraded},
		{"在庫不足", func(g *Game) { g.Cargo[stone] = 1 }, func(g *Game) error { return g.Sell(stone, 2) }, ErrNoStock},
		{"0 個の購入", nil, func(g *Game) error { return g.Buy(stone, 0) }, ErrBadAmount},
		{"負の個数の売却", func(g *Game) { g.Cargo[stone] = 1 }, func(g *Game) error { return g.Sell(stone, -1) }, ErrBadAmount},
		{"拡張の所持金不足", func(g *Game) { g.Money = upgradeBaseCost - 1 }, func(g *Game) error { return g.Upgrade() }, ErrNoMoney},
		{"現在地への出航", nil, func(g *Game) error { _, err := g.Sail(0); return err }, ErrSameIsland},
		{"Day 100 を超える出航", func(g *Game) { g.Day = LastDay - 2 }, func(g *Game) error { _, err := g.Sail(1); return err }, ErrTooLate},
		{"Day 100 での待機", func(g *Game) { g.Day = LastDay }, func(g *Game) error { return g.Wait() }, ErrTooLate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := fixture(20) // 移動日数 3
			if c.setup != nil {
				c.setup(g)
			}
			before := state(g)
			if err := c.op(g); !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
			if after := state(g); after != before {
				t.Errorf("状態が変わった\nbefore %s\nafter  %s", before, after)
			}
		})
	}

	// Quote は Buy / Sell と同じエラーを返し、所持金不足でも合計額は返す
	g := fixture(20)
	g.Money = 9999
	if total, _, err := g.Quote(stone, 1, true); total != 10000 || !errors.Is(err, ErrNoMoney) {
		t.Errorf("所持金不足の Quote = %d, %v", total, err)
	}
	g.Money = StartMoney
	if _, _, err := g.Quote(stone, 51, true); !errors.Is(err, ErrOverCapacity) {
		t.Errorf("積載量オーバーの Quote: %v", err)
	}
	if _, _, err := g.Quote(stone, 1, false); !errors.Is(err, ErrNoStock) {
		t.Errorf("在庫不足の Quote: %v", err)
	}
	if err := g.Buy(stone, 1<<40); !errors.Is(err, ErrOverCapacity) {
		t.Errorf("巨大な個数の購入: %v", err)
	}

	// 境界: 積載量ちょうどまでは買える
	g = fixture(20)
	g.Cargo[moss] = 188
	if err := g.Buy(stone, 3); err != nil {
		t.Errorf("積載量ちょうどの購入: %v", err)
	}
}

func TestEnd(t *testing.T) {
	g := fixture(20) // 移動日数 3
	g.Day = LastDay - 3
	if _, err := g.Sail(1); err != nil {
		t.Fatalf("Day 100 ちょうどに着く出航: %v", err)
	}
	if g.Day != LastDay || !g.Over() {
		t.Fatalf("Day %d, Over %v", g.Day, g.Over())
	}
	if err := g.Buy(moss, 1); err != nil {
		t.Errorf("Day 100 の購入: %v", err)
	}
	if err := g.Sell(moss, 1); err != nil {
		t.Errorf("Day 100 の売却: %v", err)
	}
	if err := g.Upgrade(); err != nil {
		t.Errorf("Day 100 の拡張: %v", err)
	}
	if g.Score() != g.Money-StartMoney || g.Score() >= 0 {
		t.Errorf("Score = %d, 所持金 %d", g.Score(), g.Money)
	}
	g.Money = StartMoney + 123
	if g.Score() != 123 {
		t.Errorf("Score = %d, want 123", g.Score())
	}
}

// randomOp は乱数で選んだ操作を 1 つ行う。エラーは無視する。
func randomOp(g *Game, r *rand.Rand) {
	good := GoodID(r.IntN(len(Goods)))
	switch r.IntN(7) {
	case 0:
		g.Buy(good, 1+r.IntN(20))
	case 1:
		g.Buy(good, g.MaxTrade(good, true))
	case 2:
		g.Sell(good, 1+r.IntN(20))
	case 3:
		g.Sell(good, g.MaxTrade(good, false))
	case 4:
		g.Sail(IslandID(r.IntN(len(g.Islands))))
	case 5:
		g.Wait()
	case 6:
		g.Upgrade()
	}
}

func TestReproducible(t *testing.T) {
	play := func(seed int64) string {
		g := New(seed)
		r := rand.New(rand.NewPCG(42, 0))
		for range 500 {
			randomOp(g, r)
		}
		return state(g)
	}
	if a, b := play(7), play(7); a != b {
		t.Errorf("同じシードと操作で状態が異なる\n%s\n%s", a, b)
	}
	if play(7) == play(8) {
		t.Error("シードが違うのに状態が同じ")
	}
}

func TestInvariants(t *testing.T) {
	r := rand.New(rand.NewPCG(99, 0))
	for seed := range int64(20) {
		g := New(seed)
		for i := range 5000 {
			randomOp(g, r)
			if g.Over() && r.IntN(10) == 0 {
				g = New(seed + int64(i)*1000) // 最終日に着いたら別の世界でやり直す
			}
			if g.Money < 0 || g.Load() > g.Capacity || g.Day < 1 || g.Day > LastDay {
				t.Fatalf("seed %d op %d: 不変条件が崩れた: %s", seed, i, state(g))
			}
			for j, n := range g.Cargo {
				if n < 0 || g.Cost[j] < 0 || (n == 0) != (g.Cost[j] == 0) {
					t.Fatalf("seed %d op %d: 積荷 %v と仕入れ額 %v が合わない", seed, i, g.Cargo, g.Cost)
				}
			}
			for _, is := range g.Islands {
				if !inSky(is.Pos) {
					t.Fatalf("seed %d op %d: 島が空域外 %v", seed, i, is.Pos)
				}
			}
		}
	}
}

// TestBalance は名産 → 需要の 1 回の航海で、初期の積載量いっぱいに運ぶと利益が出ることを確かめる。
// 定数を調整してバランスが崩れたときに気づくためのもの。
func TestBalance(t *testing.T) {
	route := func() *Game {
		g := fixture(8) // 移動日数 1
		for j := range Goods {
			g.Islands[0].Market[j] = newMarket(GoodID(j), AttrSpecialty)
			g.Islands[1].Market[j] = newMarket(GoodID(j), AttrDemand)
		}
		return g
	}
	voyage := func(g *Game) int {
		if _, err := g.Sail(1); err != nil {
			t.Fatal(err)
		}
		for j, n := range g.Cargo {
			if n > 0 {
				if err := g.Sell(GoodID(j), n); err != nil {
					t.Fatal(err)
				}
			}
		}
		return g.Score()
	}

	single := route()
	if err := single.Buy(scale, single.MaxTrade(scale, true)); err != nil || single.Load() != startCapacity {
		t.Fatalf("竜の鱗を積載量いっぱいに積めない: %v, 積荷 %d", err, single.Load())
	}
	singleProfit := voyage(single)
	if singleProfit <= 0 {
		t.Errorf("竜の鱗のみ: 利益 %d", singleProfit)
	}

	// 貪欲法: 重さあたりの限界利益が最大の品目を 1 個ずつ積む
	mixed := route()
	buyP, sellP := make([]int, len(Goods)), make([]int, len(Goods))
	for j := range Goods {
		buyP[j], sellP[j] = mixed.Islands[0].Market[j].Price, mixed.Islands[1].Market[j].Price
	}
	for {
		best, bestGain := -1, 0.0
		for j, gd := range Goods {
			gain, _ := trade(sellP[j], false)
			if per := float64(gain-buyP[j]) / float64(gd.Weight); per > bestGain && mixed.Load()+gd.Weight <= mixed.Capacity {
				best, bestGain = j, per
			}
		}
		if best < 0 {
			break
		}
		if err := mixed.Buy(GoodID(best), 1); err != nil {
			t.Fatal(err)
		}
		_, buyP[best] = trade(buyP[best], true)
		_, sellP[best] = trade(sellP[best], false)
	}
	mixedProfit := voyage(mixed)
	t.Logf("竜の鱗のみ %d G, 積み分け %d G (積荷 %v)", singleProfit, mixedProfit, mixed.Cargo)
	if mixedProfit <= singleProfit {
		t.Errorf("積み分けても得をしない: %d <= %d", mixedProfit, singleProfit)
	}
}
