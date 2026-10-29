package game

import "math/rand/v2"

// Market は島 × 商品ごとの相場。金額はすべて整数(G)。
type Market struct {
	Attr  Attr
	Init  int // 基準価格の初期値。揺らぎの範囲の中心
	Base  int // 基準価格。現在価格はこれに向かって戻る
	Price int // 現在価格 = 買値
}

func newMarket(good GoodID, a Attr) Market {
	p := Goods[good].Base * Attrs[a].Pct / 100
	return Market{Attr: a, Init: p, Base: p, Price: p}
}

func (m *Market) Traded() bool { return m.Attr != AttrNone }

func (m *Market) SellPrice() int {
	amount, _ := trade(m.Price, false)
	return amount
}

// trade は現在価格 price で 1 個売買したときの金額と、その後の現在価格を返す。
func trade(price int, buy bool) (amount, next int) {
	if buy {
		return price, price * buyImpactPct / 100
	}
	return price * sellPct / 100, price * sellImpactPct / 100
}

// quote は現在価格 price から n 個を 1 個ずつ順に売買した合計額と、その後の現在価格を返す。
func quote(price, n int, buy bool) (total, next int) {
	for range n {
		amount, next := trade(price, buy)
		total, price = total+amount, next
	}
	return total, price
}

// fluctuate は基準価格を ±fluctuationRate だけ揺らし、初期値の ±fluctuationBandPct に収める。
func (m *Market) fluctuate(rng *rand.Rand) {
	r := (rng.Float64()*2 - 1) * fluctuationRate
	base := int(float64(m.Base) * (1 + r))
	lo := m.Init * (100 - fluctuationBandPct) / 100
	hi := m.Init * (100 + fluctuationBandPct) / 100
	m.Base = min(max(base, lo), hi)
}

// restore は現在価格を基準価格へ近づける。差が小さいと切り捨てで動かなくなるが、許容する。
func (m *Market) restore() {
	m.Price += (m.Base - m.Price) * restorePct / 100
}

// genMarkets は島ごとの属性を、条件を満たすまで全島ぶん引き直して相場を作る。
func genMarkets(rng *rand.Rand, isl []Island) {
	for i := range isl {
		isl[i].Market = make([]Market, len(Goods))
	}
	for {
		for i := range isl {
			ms := isl[i].Market
			for j := range ms {
				ms[j].Attr = Attr(rng.IntN(int(AttrNone)))
			}
			if rng.Float64() < notTradedProb {
				ms[rng.IntN(len(ms))].Attr = AttrNone
			}
		}
		if validAttrs(isl) {
			break
		}
	}
	for i := range isl {
		for j, m := range isl[i].Market {
			isl[i].Market[j] = newMarket(GoodID(j), m.Attr)
		}
	}
}

// validAttrs は、各島と各商品に名産と需要がそれぞれ 1 つ以上あるかを返す。
func validAttrs(isl []Island) bool {
	// 添字は Attr。名産と需要は 0 と 1 なので [2]bool に収まる
	islandHas := make([][2]bool, len(isl))
	goodHas := make([][2]bool, len(Goods))
	for i := range isl {
		for j, m := range isl[i].Market {
			if m.Attr == AttrSpecialty || m.Attr == AttrDemand {
				islandHas[i][m.Attr] = true
				goodHas[j][m.Attr] = true
			}
		}
	}
	for _, has := range append(islandHas, goodHas...) {
		if !has[AttrSpecialty] || !has[AttrDemand] {
			return false
		}
	}
	return true
}
