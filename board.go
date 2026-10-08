package main

// Board layout (classic structure; square names intentionally omitted).
// 40 squares. Corners: 0=GO, 10=JAIL, 20=PARKING, 30=GO_TO_JAIL, plus 39=GO_TO_JAIL.
// Color groups: g0(2), g1(3), g2(3), g3(3), g4(3), g5(2), g6(2), g7(2).

type Kind int

const (
	KGo Kind = iota
	KProperty
	KTax
	KRailroad
	KUtility
	KChance
	KChest
	KParking
	KJail
	KGoToJail
)

type Square struct {
	Kind  Kind   `json:"kind"`
	Group int    `json:"group"`
	Buy   int    `json:"buy"`
	Rent  [6]int `json:"rent"` // 0=base, 1..4=houses, 5=hotel
	Tax   int    `json:"tax"`
}

var board [40]Square

func init() { buildBoard() }

func buildBoard() {
	type pd struct {
		group int
		buy   int
		rents [6]int
	}
	props := map[int]pd{
		1:  {0, 60, [6]int{2, 10, 30, 90, 160, 250}},
		3:  {0, 60, [6]int{4, 20, 60, 180, 400, 450}},
		6:  {1, 100, [6]int{6, 30, 90, 270, 400, 450}},
		8:  {1, 100, [6]int{6, 30, 90, 270, 400, 450}},
		9:  {1, 120, [6]int{8, 40, 100, 300, 450, 600}},
		11: {2, 140, [6]int{10, 50, 150, 450, 625, 750}},
		13: {2, 140, [6]int{10, 50, 150, 450, 625, 750}},
		15: {2, 160, [6]int{12, 60, 180, 500, 700, 900}},
		17: {3, 180, [6]int{14, 70, 200, 550, 750, 950}},
		18: {3, 180, [6]int{14, 70, 200, 550, 750, 950}},
		21: {3, 200, [6]int{16, 80, 220, 600, 800, 1000}},
		23: {4, 220, [6]int{18, 90, 250, 700, 875, 1050}},
		24: {4, 220, [6]int{18, 90, 250, 700, 875, 1050}},
		26: {4, 240, [6]int{20, 100, 300, 750, 925, 1100}},
		27: {5, 260, [6]int{22, 110, 330, 800, 975, 1150}},
		29: {5, 260, [6]int{22, 110, 330, 800, 975, 1150}},
		31: {6, 280, [6]int{24, 120, 360, 850, 1025, 1200}},
		33: {6, 300, [6]int{26, 130, 390, 900, 1100, 1300}},
		35: {7, 350, [6]int{35, 175, 500, 1100, 1300, 1500}},
		36: {7, 400, [6]int{50, 200, 600, 1400, 1700, 2000}},
	}
	for sq, d := range props {
		board[sq] = Square{Kind: KProperty, Group: d.group, Buy: d.buy, Rent: d.rents}
	}
	for _, sq := range []int{5, 14, 25, 38} {
		board[sq] = Square{Kind: KRailroad, Buy: 200}
	}
	for _, sq := range []int{12, 28} {
		board[sq] = Square{Kind: KUtility, Buy: 150}
	}
	board[0] = Square{Kind: KGo}
	board[10] = Square{Kind: KJail}
	board[20] = Square{Kind: KParking}
	board[30] = Square{Kind: KGoToJail}
	board[39] = Square{Kind: KGoToJail}
	board[4] = Square{Kind: KTax, Tax: 200}
	board[19] = Square{Kind: KTax, Tax: 100}
	board[37] = Square{Kind: KTax, Tax: 100}
	for _, sq := range []int{2, 16, 32} {
		board[sq] = Square{Kind: KChest}
	}
	for _, sq := range []int{7, 22, 34} {
		board[sq] = Square{Kind: KChance}
	}
}

var groups [][]int

func init() {
	groups = make([][]int, 8)
	for i := 0; i < 40; i++ {
		if board[i].Kind == KProperty {
			groups[board[i].Group] = append(groups[board[i].Group], i)
		}
	}
}
