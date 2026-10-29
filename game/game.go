// Package game は skyisle-trader のゲームロジック。UI に依存しない。
//
// 状態は exported なフィールドで公開するが、変更してよいのはメソッドだけ。
// 不正な操作は番兵エラーを返し、状態を一切変えない。
package game

import (
	"errors"
	"math/rand/v2"
)

// 調整用の数値。割合は整数演算で端数を 0 方向へ切り捨てるため、百分率で持つ。
const (
	StartMoney      = 1_000_000 // 開始時の所持金
	LastDay         = 100       // 最終日。この日を超える出航と、この日の待機はできない
	startCapacity   = 200       // 積載量の初期値
	UpgradeStep     = 100       // 拡張 1 回で増える積載量
	upgradeBaseCost = 200_000   // n 回目の拡張費用は upgradeBaseCost × n

	mapW, mapH         = 60.0, 40.0 // 空域の広さ(論理座標)
	minIslandGap       = 10.0       // 初期配置での島同士の最小距離
	minDrift, maxDrift = 0.5, 2.0   // 島の速さ(単位/日)
	shipSpeed          = 8.0        // 飛行船の速さ(単位/日)

	notTradedProb = 0.5 // 島が 1 品目を取扱なしにする確率

	sellPct            = 90   // 売値 = 現在価格 × 90%
	buyImpactPct       = 101  // 1 個買うごとに現在価格 × 101%
	sellImpactPct      = 99   // 1 個売るごとに現在価格 × 99%
	fluctuationRate    = 0.05 // 基準価格の日次の揺らぎ幅(±)
	fluctuationBandPct = 30   // 基準価格は初期値の ±30% に収める
	restorePct         = 15   // 現在価格は毎日、基準価格との差の 15% だけ戻る
)

// GoodID は Goods の添字。
type GoodID int

// IslandID は IslandDefs と Game.Islands の添字。
type IslandID int

type GoodDef struct {
	Name   string
	Short  string // 出航先の一覧で使う 1 文字の略称
	Base   int    // 基準価格
	Weight int    // 1 個が消費する積載量
}

var Goods = []GoodDef{
	{"浮遊苔", "苔", 3000, 1},
	{"霧酒", "酒", 8000, 3},
	{"魔石", "石", 10000, 4},
	{"星砂", "砂", 20000, 9},
	{"竜の鱗", "鱗", 40000, 20},
}

type IslandDef struct {
	Name string
	Mark string // 地図に出す 1 文字。島ごとに異なること
}

var IslandDefs = []IslandDef{
	{"霧の島", "霧"},
	{"翠嶺島", "翠"},
	{"星見島", "星"},
	{"竜骨島", "竜"},
	{"灰海島", "灰"},
	{"鐘楼島", "鐘"},
	{"風鳴島", "風"},
	{"白鯨島", "白"},
}

// Attr は島 × 商品ごとの属性。Attrs の添字。
type Attr int

const (
	AttrSpecialty Attr = iota // 名産
	AttrDemand                // 需要
	AttrNormal                // 通常
	AttrNone                  // 取扱なし。生成時に等確率で選ばれるのは、これより前の属性
)

type AttrDef struct {
	Name  string
	Short string // 出航先の一覧で使う 1 文字の略称
	Pct   int    // 基準価格の倍率(%)
}

var Attrs = []AttrDef{
	AttrSpecialty: {"名産", "名", 60},
	AttrDemand:    {"需要", "需", 160},
	AttrNormal:    {"通常", "通", 100},
	AttrNone:      {"取扱なし", "－", 0},
}

var (
	ErrNoMoney      = errors.New("所持金が足りません")
	ErrOverCapacity = errors.New("積載量を超えます")
	ErrNotTraded    = errors.New("この島では取り扱っていません")
	ErrNoStock      = errors.New("積荷が足りません")
	ErrBadAmount    = errors.New("個数は 1 以上を指定してください")
	ErrSameIsland   = errors.New("現在地には出航できません")
	ErrTooLate      = errors.New("最終日を過ぎてしまいます")
)

type Game struct {
	Seed     int64
	Day      int
	Money    int
	Here     IslandID
	Cargo    []int // GoodID ごとの個数
	Cost     []int // GoodID ごとの、今の積荷の仕入れ額の合計。表示用で、ルールには影響しない
	Capacity int
	Upgrades int      // 拡張済みの回数
	Islands  []Island // IslandID ごと

	// 生成にも日次の揺らぎにも、この 1 つだけを使う。
	// 同じシードと同じ操作の列からは必ず同じ結果になる。
	rng *rand.Rand
}

// New は seed から世界を生成する。
func New(seed int64) *Game {
	g := &Game{
		Seed:     seed,
		Day:      1,
		Money:    StartMoney,
		Capacity: startCapacity,
		Cargo:    make([]int, len(Goods)),
		Cost:     make([]int, len(Goods)),
		rng:      rand.New(rand.NewPCG(uint64(seed), 0)),
	}
	g.Islands = genIslands(g.rng)
	genMarkets(g.rng, g.Islands)
	g.Here = IslandID(g.rng.IntN(len(g.Islands)))
	return g
}

// Load は積荷の重さの合計。
func (g *Game) Load() int {
	w := 0
	for i, n := range g.Cargo {
		w += n * Goods[i].Weight
	}
	return w
}

func (g *Game) market(good GoodID) *Market {
	return &g.Islands[g.Here].Market[good]
}

// Quote は現在地で good を n 個売買したときの合計額と、取引後の現在価格を返す。状態は変えない。
// エラーは Buy / Sell が返すのと同じ。所持金不足のときは、合計額と価格を返したうえでエラーにする。
// 金額は積載量と在庫の検証を通ったときだけ計算する(n が積載量か在庫の数で抑えられ、ループが有限になる)。
func (g *Game) Quote(good GoodID, n int, buy bool) (total, price int, err error) {
	m := g.market(good)
	switch {
	case !m.Traded():
		return 0, 0, ErrNotTraded
	case n < 1:
		return 0, 0, ErrBadAmount
	case buy && g.Load()+n*Goods[good].Weight > g.Capacity:
		return 0, 0, ErrOverCapacity
	case !buy && n > g.Cargo[good]:
		return 0, 0, ErrNoStock
	}
	total, price = quote(m.Price, n, buy)
	if buy && total > g.Money {
		err = ErrNoMoney
	}
	return total, price, err
}

func (g *Game) Buy(good GoodID, n int) error {
	total, price, err := g.Quote(good, n, true)
	if err != nil {
		return err
	}
	g.Money -= total
	g.Cargo[good] += n
	g.Cost[good] += total
	g.market(good).Price = price
	return nil
}

func (g *Game) Sell(good GoodID, n int) error {
	total, price, err := g.Quote(good, n, false)
	if err != nil {
		return err
	}
	g.Money += total
	g.Cost[good] -= g.CostOf(good, n)
	g.Cargo[good] -= n
	g.market(good).Price = price
	return nil
}

// CostOf は積荷の good n 個分の仕入れ額。平均原価で按分するので、全部売れば仕入れ額は 0 に戻る。
func (g *Game) CostOf(good GoodID, n int) int {
	if g.Cargo[good] == 0 {
		return 0
	}
	return g.Cost[good] * n / g.Cargo[good]
}

// Estimate は dest で売れる積荷をすべて売ったときの売却額の目安と、その仕入れ額を返す。
// 他の島の現在価格は見えないので、公開されている属性どおりの価格(Market.Init)から計算する。
func (g *Game) Estimate(dest IslandID) (sales, cost int) {
	for j, n := range g.Cargo {
		if m := g.Islands[dest].Market[j]; n > 0 && m.Traded() {
			total, _ := quote(m.Init, n, false)
			sales += total
			cost += g.Cost[j]
		}
	}
	return sales, cost
}

// MaxTrade は現在地で good を売買できる最大の個数を返す。
// 買う場合は所持金と積載量で、売る場合は積荷の数で決まる。
func (g *Game) MaxTrade(good GoodID, buy bool) int {
	m := g.market(good)
	if !m.Traded() {
		return 0
	}
	if !buy {
		return g.Cargo[good]
	}
	limit := (g.Capacity - g.Load()) / Goods[good].Weight
	n, total, price := 0, 0, m.Price
	for n < limit {
		amount, next := trade(price, true)
		if total+amount > g.Money {
			break
		}
		n, total, price = n+1, total+amount, next
	}
	return n
}

func (g *Game) UpgradeCost() int {
	return upgradeBaseCost * (g.Upgrades + 1)
}

func (g *Game) Upgrade() error {
	cost := g.UpgradeCost()
	if cost > g.Money {
		return ErrNoMoney
	}
	g.Money -= cost
	g.Capacity += UpgradeStep
	g.Upgrades++
	return nil
}

// Sail は dest へ出航し、到着までの 1 日ごとの Snapshot を返す。
func (g *Game) Sail(dest IslandID) ([]Snapshot, error) {
	if dest == g.Here {
		return nil, ErrSameIsland
	}
	days, arrival := g.Route(dest)
	if g.Day+days > LastDay {
		return nil, ErrTooLate
	}
	from := g.Islands[g.Here].Pos
	snaps := make([]Snapshot, days)
	for i := range snaps {
		g.advanceDay()
		snaps[i] = Snapshot{
			Day:     g.Day,
			Islands: g.positions(),
			Ship:    lerp(from, arrival, float64(i+1)/float64(days)),
		}
	}
	g.Here = dest
	return snaps, nil
}

// Wait はその場で 1 日待つ。
func (g *Game) Wait() error {
	if g.Over() {
		return ErrTooLate
	}
	g.advanceDay()
	return nil
}

func (g *Game) Score() int { return g.Money - StartMoney }

// Over は最終日に達したら true。その後も売買と拡張はできる。
func (g *Game) Over() bool { return g.Day >= LastDay }

// advanceDay は日付を 1 日進める。日付が進むのはここだけ。
func (g *Game) advanceDay() {
	g.Day++
	for i := range g.Islands {
		is := &g.Islands[i]
		is.Pos, is.Vel = drift(is.Pos, is.Vel)
		for j := range is.Market {
			if m := &is.Market[j]; m.Traded() {
				m.fluctuate(g.rng)
				m.restore()
			}
		}
	}
}
