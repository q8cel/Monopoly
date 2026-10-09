package main

// ---- 2D board: a 9x9 grid (x: 0..8 left->right, y: 0..8 bottom->top) ----
//
// Cell kinds:
//   A B C D  player bases (corners, assigned to players in join order)
//   a b c e f g h i   property cells, 2x2 blocks (8 groups)
//   H  highways (railroad analog: rent doubles per owned highway)
//   $  bank (+$100), J  stockade (jail), P  parking (rest)
//   T  tax, !  chance, ?  community chest, .  open ground (parking)

const Grid = 9
const CellCount = Grid * Grid

type Kind int

const (
	KBase Kind = iota
	KProperty
	KHighway
	KBank
	KJail
	KChance
	KChest
	KTax
	KParking
)

type Cell struct {
	X      int  `json:"x"`
	Y      int  `json:"y"`
	Kind   Kind `json:"kind"`
	Group  int  `json:"group"`
	Buy    int  `json:"buy"`
	Rent   [5]int `json:"rent"`
	Tax    int  `json:"tax"`
	Owner  int  `json:"owner"`
	Houses int  `json:"houses"`
	Mortg  bool `json:"mortg"`
}

func (c *Cell) Id() int { return c.Y*Grid + c.X }

// map lines, top of the board (y=8) down to the bottom (y=0)
var mapLines = [Grid]string{
	"A?!?!?!?B",
	"!aabbHcc?",
	"Taabb.ccT",
	"?H!!?P?H!",
	"!eeffJ???",
	"?eeff$!!!",
	"Tgghh.iiT",
	"!gghhHii?",
	"C?!?!?!?D",
}

type groupDef struct {
	buy  int
	rent [5]int
}

var groupTable = map[byte]groupDef{
	'a': {100, [5]int{20, 40, 80, 120, 160}},
	'b': {120, [5]int{25, 50, 100, 150, 200}},
	'c': {140, [5]int{30, 60, 120, 180, 240}},
	'e': {160, [5]int{35, 70, 140, 210, 280}},
	'f': {180, [5]int{40, 80, 160, 240, 320}},
	'g': {200, [5]int{45, 90, 180, 270, 360}},
	'h': {220, [5]int{50, 100, 200, 300, 400}},
	'i': {240, [5]int{55, 110, 220, 330, 440}},
}

var groupOf = map[byte]int{'a': 0, 'b': 1, 'c': 2, 'e': 3, 'f': 4, 'g': 5, 'h': 6, 'i': 7}

// Base cell id for each player slot (join order): A(0,8) B(8,8) C(0,0) D(8,0)
var slotBase = [4]int{8*Grid + 0, 8*Grid + 8, 0, 8}

func NewCells() [CellCount]Cell {
	var cells [CellCount]Cell
	for i := range cells {
		cells[i].Owner = -1
	}
	for ly, line := range mapLines {
		y := Grid - 1 - ly
		for x := 0; x < Grid; x++ {
			c := &cells[y*Grid+x]
			c.X, c.Y = x, y
			switch ch := line[x]; ch {
			case 'A', 'B', 'C', 'D':
				c.Kind = KBase
			case 'H':
				c.Kind = KHighway
				c.Buy = 200
			case '$':
				c.Kind = KBank
			case 'J':
				c.Kind = KJail
			case 'P', '.':
				c.Kind = KParking
			case 'T':
				c.Kind = KTax
				c.Tax = 100
			case '!':
				c.Kind = KChance
			case '?':
				c.Kind = KChest
			default:
				if g, ok := groupOf[ch]; ok {
					c.Kind = KProperty
					c.Group = g
					c.Buy, c.Rent = groupTable[ch].buy, groupTable[ch].rent
				}
			}
		}
	}
	return cells
}

// Bounce1d reflects a straight move off the grid edges (billiards style):
// the unit keeps sliding, mirroring at each wall.
func Bounce1d(coord, steps int) int {
	span := Grid - 1
	v := coord + steps
	m := v % (2 * span)
	if m < 0 {
		m += 2 * span
	}
	if m > span {
		m = 2*span - m
	}
	return m
}
