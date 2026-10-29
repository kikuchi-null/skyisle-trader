package game

import (
	"math"
	"math/rand/v2"
)

type Vec struct{ X, Y float64 }

func (v Vec) Dist(w Vec) float64 { return math.Hypot(v.X-w.X, v.Y-w.Y) }

func lerp(a, b Vec, t float64) Vec {
	return Vec{a.X + (b.X-a.X)*t, a.Y + (b.Y-a.Y)*t}
}

type Island struct {
	Pos, Vel Vec
	Market   []Market // GoodID ごとの相場
}

// Snapshot は航行アニメーションの 1 コマ。
type Snapshot struct {
	Day     int
	Islands []Vec // IslandID ごとの位置
	Ship    Vec
}

// genIslands は島同士が minIslandGap 以上離れるまで、全島の位置を引き直す。
func genIslands(rng *rand.Rand) []Island {
	isl := make([]Island, len(IslandDefs))
	for {
		for i := range isl {
			isl[i].Pos = Vec{rng.Float64() * mapW, rng.Float64() * mapH}
		}
		if spaced(isl) {
			break
		}
	}
	for i := range isl {
		speed := minDrift + rng.Float64()*(maxDrift-minDrift)
		angle := rng.Float64() * 2 * math.Pi
		isl[i].Vel = Vec{speed * math.Cos(angle), speed * math.Sin(angle)}
	}
	return isl
}

func spaced(isl []Island) bool {
	for i := range isl {
		for j := range i {
			if isl[i].Pos.Dist(isl[j].Pos) < minIslandGap {
				return false
			}
		}
	}
	return true
}

// drift は位置と速度を 1 日ぶん進める。乱数を使わないので、未来の位置を何度でも計算できる。
func drift(p, v Vec) (Vec, Vec) {
	p.X, v.X = bounce(p.X+v.X, v.X, mapW)
	p.Y, v.Y = bounce(p.Y+v.Y, v.Y, mapH)
	return p, v
}

// bounce は空域の端 [0, limit] を超えた x を端で鏡のように反射させ、速度を反転する。
// 1 日の移動量は空域より十分小さいので、反射は 1 回で足りる。
func bounce(x, v, limit float64) (float64, float64) {
	switch {
	case x < 0:
		return -x, -v
	case x > limit:
		return 2*limit - x, -v
	}
	return x, v
}

// Route は dest への移動日数と、到着時の dest の位置(合流点)を返す。
// 出航元の位置は出航時点で固定し、d 日後の dest に 8d 以内で届く最小の d を探す。
// 飛行船は島より速く、空域は有限なので必ず見つかる。
func (g *Game) Route(dest IslandID) (int, Vec) {
	from := g.Islands[g.Here].Pos
	p, v := g.Islands[dest].Pos, g.Islands[dest].Vel
	for d := 1; ; d++ {
		p, v = drift(p, v)
		if from.Dist(p) <= shipSpeed*float64(d) {
			return d, p
		}
	}
}

// TravelDays は現在地から dest への移動日数。
func (g *Game) TravelDays(dest IslandID) int {
	d, _ := g.Route(dest)
	return d
}

func (g *Game) positions() []Vec {
	ps := make([]Vec, len(g.Islands))
	for i, is := range g.Islands {
		ps[i] = is.Pos
	}
	return ps
}
