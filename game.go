package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// ---- constants ----
const (
	maxActors     = 6
	actorCost     = 300
	startTreasury = 1500
	actorFunds    = 200
	jailFine      = 50
	jailMaxTurns  = 3 // auto release after this many jailed turns
	bankReward    = 100
	baseReward    = 100 // landing on your own base
	baseToll      = 50  // landing on an enemy base
	occupyFee     = 25  // per enemy unit on the landed cell (+ half base rent on properties)
	maxOccupy     = 3   // max enemy units charged per landing
	upkeepPerUnit = 10  // per living unit, paid to the bank at the start of each turn
	maxHouses     = 4
	houseCost     = 50
	houseSell     = 25
	auctionStep   = 10
)

// ---- actors (units) ----

type Actor struct {
	ID        int  `json:"id"`
	Owner     int  `json:"owner"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Cash      int  `json:"cash"`
	Jailed    bool `json:"jailed"`
	JailTurns int  `json:"jailedTurns"`
	Alive     bool `json:"alive"`
}

// ---- players ----

type Player struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Cash     int    `json:"cash"` // treasury
	Base     int    `json:"base"`
	Bankrupt bool   `json:"bankrupt"`
	Left     bool   `json:"left"`
}

// ---- deals ----

type Asset struct {
	Cash  int   `json:"cash"`
	Props []int `json:"props"`
}

type Deal struct {
	ID     int    `json:"id"`
	From   int    `json:"from"`
	To     int    `json:"to"`
	Give   Asset  `json:"give"` // From -> To
	Want   Asset  `json:"want"` // To -> From
	Status string `json:"status"`
}

// ---- phases ----

type BuyOffer struct {
	Actor int `json:"actor"`
	Cell  int `json:"cell"`
}

type Auction struct {
	Cell   int   `json:"cell"`
	Bid    int   `json:"bid"`
	Bidder int   `json:"bidder"` // -1 = nobody yet
	Passed []int `json:"passed"`
	From   int   `json:"-"` // the player whose turn the auction interrupts
}

type State struct {
	Phase    string           `json:"phase"` // lobby | roll | move | offer | auction | over
	Host     int              `json:"host"`
	Players  [4]Player        `json:"players"`
	Actors   []Actor          `json:"actors"`
	Cells    [CellCount]Cell  `json:"cells"`
	Current  int              `json:"current"`
	Dice     [2]int           `json:"dice"`
	Offer    *BuyOffer        `json:"offer,omitempty"`
	Auction  *Auction         `json:"auction,omitempty"`
	Deals    []Deal           `json:"deals"`
	Log      []string         `json:"log"`
	Winner   int              `json:"winner"`
	NextID   int              `json:"-"`
	NextDeal int              `json:"-"`
}

type Game struct {
	mu  sync.Mutex
	St  *State
	rng *rand.Rand
}

func NewGame() *Game {
	g := &Game{rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	g.resetState()
	g.St.Phase = "lobby"
	return g
}

func (g *Game) resetState() {
	g.St = &State{
		Host:    -1,
		Current: -1,
		Winner:  -1,
		Cells:   NewCells(),
	}
	for i := range g.St.Players {
		g.St.Players[i].ID = i
		g.St.Players[i].Base = slotBase[i]
	}
}

// Snapshot returns a JSON copy of the state (safe to send on sockets).
func (g *Game) Snapshot() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	b, _ := json.Marshal(g.St)
	return b
}

func (g *Game) log(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	g.St.Log = append(g.St.Log, msg)
	if len(g.St.Log) > 120 {
		g.St.Log = g.St.Log[len(g.St.Log)-120:]
	}
}

// ---- lobby / lifecycle ----

func (g *Game) Join(name string) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase == "over" {
		g.toLobbyLocked()
	}
	if g.St.Phase != "lobby" {
		return -1, errors.New("game already in progress")
	}
	slot := -1
	for i := range g.St.Players {
		if g.St.Players[i].Left || g.St.Players[i].Name == "" {
			slot = i
			break
		}
	}
	if slot == -1 {
		return -1, errors.New("lobby is full")
	}
	if name == "" {
		name = fmt.Sprintf("Player %d", slot+1)
	}
	g.St.Players[slot].Name = name
	g.St.Players[slot].Left = false
	if g.St.Host == -1 {
		g.St.Host = slot
	}
	g.log("%s joined the lobby", name)
	return slot, nil
}

func (g *Game) Start() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "lobby" {
		return errors.New("game is not in the lobby")
	}
	if g.St.Host < 0 {
		return errors.New("no host yet")
	}
	n := 0
	for _, p := range g.St.Players {
		if !p.Left && p.Name != "" {
			n++
		}
	}
	if n < 2 {
		return errors.New("need at least 2 players")
	}
	g.St.Cells = NewCells()
	g.St.Actors = nil
	g.St.NextID = 0
	g.St.Deals = nil
	g.St.Offer = nil
	g.St.Auction = nil
	g.St.Winner = -1
	g.St.Log = nil
	first := -1
	for i := range g.St.Players {
		p := &g.St.Players[i]
		p.Bankrupt = false
		p.Base = slotBase[i]
		if p.Left || p.Name == "" {
			continue
		}
		p.Cash = startTreasury
		if first == -1 {
			first = i
		}
		for k := 0; k < 3; k++ {
			g.St.Actors = append(g.St.Actors, Actor{
				ID:    g.St.NextID,
				Owner: i,
				X:     p.Base % Grid,
				Y:     p.Base / Grid,
				Cash:  actorFunds,
				Alive: true,
			})
			g.St.NextID++
		}
	}
	g.St.Current = first
	g.St.Phase = "roll"
	g.log("The battle begins! %d armies deploy.", n)
	return nil
}

func (g *Game) ResetToLobby() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "over" {
		return errors.New("game is still running")
	}
	g.toLobbyLocked()
	return nil
}

func (g *Game) toLobbyLocked() {
	for i := range g.St.Players {
		p := &g.St.Players[i]
		if !p.Left {
			p.Cash = 0
			p.Bankrupt = false
		}
		p.Name = ""
	}
	g.St.Cells = NewCells()
	g.St.Actors = nil
	g.St.Offer = nil
	g.St.Auction = nil
	g.St.Deals = nil
	g.St.Winner = -1
	g.St.Current = -1
	g.St.Host = -1
	g.St.Log = nil
	g.St.Phase = "lobby"
}

// ---- connectivity / host removal ----

func (g *Game) Disconnect(pid int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.removeLocked(pid)
}

func (g *Game) RemovePlayer(by, pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if by != g.St.Host {
		return errors.New("only the host can remove players")
	}
	if pid == by {
		return errors.New("cannot remove yourself")
	}
	if pid < 0 || pid >= len(g.St.Players) || g.St.Players[pid].Left {
		return errors.New("no such player")
	}
	g.removeLocked(pid)
	return nil
}

func (g *Game) removeLocked(pid int) {
	if pid < 0 || pid >= len(g.St.Players) || g.St.Players[pid].Left {
		return
	}
	if g.St.Phase == "lobby" {
		g.log("%s was removed from the lobby.", g.St.Players[pid].Name)
		g.St.Players[pid].Left = true
		g.St.Players[pid].Name = ""
	} else {
		p := &g.St.Players[pid]
		if !p.Bankrupt {
			g.log("%s left the battlefield and is eliminated.", p.Name)
			g.bankruptTo(pid, -1)
		}
	}
	g.reassignHostLocked()
}

func (g *Game) reassignHostLocked() {
	if g.St.Host >= 0 && g.St.Host < len(g.St.Players) && !g.St.Players[g.St.Host].Left {
		return
	}
	g.St.Host = -1
	for i := range g.St.Players {
		if !g.St.Players[i].Left && g.St.Players[i].Name != "" {
			g.St.Host = i
			g.log("%s is the new host.", g.St.Players[i].Name)
			return
		}
	}
}

// ---- turn flow ----

func (g *Game) Roll() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "roll" {
		return errors.New("not the roll phase")
	}
	p := &g.St.Players[g.St.Current]
	if p.Bankrupt || p.Left {
		return errors.New("not your turn")
	}
	g.payUpkeepLocked(p)
	if g.St.Phase == "over" {
		return nil
	}
	if p.Bankrupt {
		g.nextPlayer()
		return nil
	}
	g.St.Dice = [2]int{1 + g.rng.Intn(6), 1 + g.rng.Intn(6)}
	g.log("%s rolls %d + %d = %d", p.Name, g.St.Dice[0], g.St.Dice[1], g.St.Dice[0]+g.St.Dice[1])
	movable := false
	for _, a := range g.St.Actors {
		if a.Owner == p.ID && a.Alive && !a.Jailed {
			movable = true
			break
		}
	}
	if !movable {
		g.log("%s has no free units; the turn is lost.", p.Name)
		g.nextPlayer()
		return nil
	}
	g.St.Phase = "move"
	return nil
}

// payUpkeepLocked charges the player $upkeepPerUnit for every living unit.
// If the treasury can't cover it, the poorest unit is abandoned (captured
// by the bank) until the bill is paid or the army is gone.
func (g *Game) payUpkeepLocked(p *Player) {
	for {
		units := g.aliveUnits(p.ID)
		if units == 0 {
			return
		}
		cost := upkeepPerUnit * units
		if p.Cash >= cost {
			p.Cash -= cost
			return
		}
		p.Cash = 0
		worst := -1
		for i := range g.St.Actors {
			a := &g.St.Actors[i]
			if a.Owner == p.ID && a.Alive && (worst < 0 || a.Cash < g.St.Actors[worst].Cash) {
				worst = i
			}
		}
		if worst < 0 {
			return
		}
		g.log("%s can't pay army upkeep; unit #%d is lost to the field", p.Name, g.St.Actors[worst].ID)
		g.capture(&g.St.Actors[worst], -1)
		if g.St.Phase == "over" {
			return
		}
	}
}

// Move slides one unit in one of the 8 compass directions by the dice sum,
// bouncing off the walls.
func (g *Game) Move(actorID, dx, dy int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "move" {
		return errors.New("not the move phase")
	}
	if dx < -1 || dx > 1 || dy < -1 || dy > 1 || (dx == 0 && dy == 0) {
		return errors.New("bad direction")
	}
	a := g.findActor(actorID)
	if a == nil || !a.Alive || a.Jailed {
		return errors.New("bad unit")
	}
	if a.Owner != g.St.Current {
		return errors.New("not your unit")
	}
	total := g.St.Dice[0] + g.St.Dice[1]
	nx, ny := a.X, a.Y
	if dx != 0 {
		nx = Bounce1d(a.X, dx*total)
	}
	if dy != 0 {
		ny = Bounce1d(a.Y, dy*total)
	}
	a.X, a.Y = nx, ny
	g.log("%s slides unit #%d %s by %d to (%d,%d)",
		g.St.Players[a.Owner].Name, a.ID, dirName(dx, dy), total, nx, ny)
	g.resolveLanding(a)
	return nil
}

func dirName(dx, dy int) string {
	v := map[int]string{-1: "-", 0: "·", 1: "+"}
	return "x" + v[dx] + "y" + v[dy]
}

func (g *Game) findActor(id int) *Actor {
	for i := range g.St.Actors {
		if g.St.Actors[i].ID == id {
			return &g.St.Actors[i]
		}
	}
	return nil
}

func (g *Game) baseOwner(cellID int) int {
	for i := range g.St.Players {
		if g.St.Players[i].Base == cellID && !g.St.Players[i].Left && !g.St.Players[i].Bankrupt {
			return i
		}
	}
	return -1
}

func (g *Game) monopoly(owner, group int) bool {
	if owner < 0 {
		return false
	}
	for i := range g.St.Cells {
		c := &g.St.Cells[i]
		if c.Kind == KProperty && c.Group == group && c.Owner != owner {
			return false
		}
	}
	return true
}

func (g *Game) highwayCount(owner int) int {
	n := 0
	for i := range g.St.Cells {
		c := &g.St.Cells[i]
		if c.Kind == KHighway && c.Owner == owner {
			n++
		}
	}
	return n
}

func (g *Game) groupCells(group int) []int {
	var out []int
	for i := range g.St.Cells {
		c := &g.St.Cells[i]
		if c.Kind == KProperty && c.Group == group {
			out = append(out, i)
		}
	}
	return out
}

func (g *Game) resolveLanding(a *Actor) {
	c := &g.St.Cells[a.Y*Grid+a.X]
	switch c.Kind {
	case KBase:
		own := g.baseOwner(c.Id())
		if own == a.Owner {
			a.Cash += baseReward
			g.log("unit #%d rests at home base: +$%d", a.ID, baseReward)
		} else if own >= 0 {
			g.log("unit #%d storms %s's fortress", a.ID, g.St.Players[own].Name)
			g.charge(a, baseToll, own)
		}
	case KProperty:
		if c.Owner == -1 {
			g.St.Offer = &BuyOffer{Actor: a.ID, Cell: c.Id()}
			g.log("unit #%d lands on unclaimed land (%d,%d) for $%d", a.ID, a.X, a.Y, c.Buy)
		} else if c.Owner != a.Owner && !c.Mortg {
			r := c.Rent[c.Houses]
			if g.monopoly(c.Owner, c.Group) {
				r *= 2
				g.log("complete block! rent doubled")
			}
			g.charge(a, r, c.Owner)
		}
	case KHighway:
		if c.Owner == -1 {
			g.St.Offer = &BuyOffer{Actor: a.ID, Cell: c.Id()}
			g.log("unit #%d lands on a highway, $%d to claim", a.ID, c.Buy)
		} else if c.Owner != a.Owner && !c.Mortg {
			r := 25 << (g.highwayCount(c.Owner) - 1)
			g.charge(a, r, c.Owner)
		}
	case KBank:
		a.Cash += bankReward
		g.log("unit #%d loots the bank: +$%d", a.ID, bankReward)
	case KJail:
		if !a.Jailed {
			a.Jailed = true
			a.JailTurns = 0
			g.log("unit #%d is detained in the stockade!", a.ID)
		}
	case KChance, KChest:
		g.card(a, c.Kind == KChance)
	case KTax:
		g.log("unit #%d pays tax $%d", a.ID, c.Tax)
		g.charge(a, c.Tax, -1)
	case KParking:
		g.log("unit #%d rests on open ground", a.ID)
	}
	if g.St.Phase == "over" {
		return
	}
	if a.Alive {
		g.occupy(a, c)
		if g.St.Phase == "over" {
			return
		}
		if g.St.Offer != nil {
			g.St.Phase = "offer"
			return
		}
	}
	// the unit may have been captured by the charges above; the turn
	// still has to advance
	g.nextPlayer()
}

// occupy charges a fee for each enemy unit standing on the landed cell.
func (g *Game) occupy(a *Actor, c *Cell) {
	fee := occupyFee
	if c.Kind == KProperty {
		fee += c.Rent[0] / 2
	}
	n := 0
	for i := range g.St.Actors {
		e := &g.St.Actors[i]
		if e.Alive && e.Owner != a.Owner && e.X == a.X && e.Y == a.Y && n < maxOccupy {
			g.log("unit #%d is blocked by %s's unit #%d: -$%d",
				a.ID, g.St.Players[e.Owner].Name, e.ID, fee)
			g.charge(a, fee, e.Owner)
			n++
			if !a.Alive || g.St.Phase == "over" {
				return
			}
		}
	}
}

func (g *Game) card(a *Actor, chance bool) {
	r := g.rng.Float64()
	kind := "chest"
	if chance {
		kind = "chance"
	}
	switch {
	case chance && r < 0.30, !chance && r < 0.35:
		a.Cash += 100
		g.log("%s: unit #%d finds a supply drop +$100", kind, a.ID)
	case chance && r < 0.50, !chance && r < 0.55:
		g.log("%s: unit #%d needs repairs -$50", kind, a.ID)
		g.charge(a, 50, -1)
	case chance && r < 0.65, !chance && r < 0.70:
		b := g.St.Players[a.Owner].Base
		a.X, a.Y = b%Grid, b/Grid
		g.log("%s: unit #%d is escorted home (%d,%d)", kind, a.ID, a.X, a.Y)
	case chance && r < 0.80, !chance && r < 0.85:
		g.log("%s: all armies tribute $20 to %s", kind, g.St.Players[a.Owner].Name)
		for i := range g.St.Players {
			p := &g.St.Players[i]
			if i != a.Owner && !p.Bankrupt && !p.Left {
				t := 20
				if p.Cash < t {
					t = p.Cash
				}
				p.Cash -= t
				g.St.Players[a.Owner].Cash += t
			}
		}
	case chance && r < 0.90, !chance && r < 0.92:
		g.log("%s: unit #%d's rations are taxed: pay $20 to each enemy", kind, a.ID)
		for i := range g.St.Players {
			p := &g.St.Players[i]
			if i != a.Owner && !p.Bankrupt && !p.Left {
				t := 20
				if g.St.Players[a.Owner].Cash < t {
					t = g.St.Players[a.Owner].Cash
				}
				if t == 0 {
					continue
				}
				g.St.Players[a.Owner].Cash -= t
				p.Cash += t
			}
		}
	default:
		if !a.Jailed {
			a.Jailed = true
			a.JailTurns = 0
			g.log("%s: unit #%d is detained in the stockade!", kind, a.ID)
		}
	}
}

// ---- paying, capture, bankruptcy ----

// payActor takes up to amount from the unit then its owner's treasury.
// Returns the shortfall.
func (g *Game) payActor(a *Actor, amount, to int) int {
	paid := 0
	if a.Cash >= amount {
		a.Cash -= amount
		paid = amount
		amount = 0
	}
	if amount > 0 {
		paid += a.Cash
		amount -= a.Cash
		a.Cash = 0
	}
	if amount > 0 {
		own := &g.St.Players[a.Owner]
		if own.Cash >= amount {
			own.Cash -= amount
			paid += amount
			amount = 0
		} else {
			paid += own.Cash
			amount -= own.Cash
			own.Cash = 0
		}
	}
	if to >= 0 && paid > 0 {
		g.St.Players[to].Cash += paid
	}
	return amount
}

// charge makes a unit pay; on shortfall the unit is captured.
func (g *Game) charge(a *Actor, amount, to int) {
	if amount <= 0 {
		return
	}
	short := g.payActor(a, amount, to)
	if short > 0 {
		g.log("unit #%d cannot pay $%d!", a.ID, amount)
		g.capture(a, to)
	}
}

func (g *Game) capture(a *Actor, to int) {
	g.log("%s's unit #%d is captured.", g.St.Players[a.Owner].Name, a.ID)
	a.Alive = false
	if to >= 0 {
		g.St.Players[to].Cash += a.Cash
	}
	a.Cash = 0
	g.checkElimination(a.Owner, to)
}

func (g *Game) aliveUnits(pid int) int {
	n := 0
	for _, a := range g.St.Actors {
		if a.Owner == pid && a.Alive {
			n++
		}
	}
	return n
}

func (g *Game) checkElimination(pid, to int) {
	p := &g.St.Players[pid]
	if p.Bankrupt || p.Left {
		return
	}
	if g.aliveUnits(pid) == 0 {
		g.bankruptTo(pid, to)
	}
}

func (g *Game) bankruptTo(pid, to int) {
	p := &g.St.Players[pid]
	if p.Bankrupt || p.Left {
		return
	}
	g.log("%s's army is destroyed — bankrupt!", p.Name)
	p.Bankrupt = true
	for i := range g.St.Actors {
		a := &g.St.Actors[i]
		if a.Owner == pid && a.Alive {
			a.Alive = false
			if to >= 0 {
				g.St.Players[to].Cash += a.Cash
			}
			a.Cash = 0
		}
	}
	if to >= 0 {
		w := &g.St.Players[to]
		w.Cash += p.Cash
		for i := range g.St.Cells {
			c := &g.St.Cells[i]
			if c.Owner == pid {
				w.Cash += c.Houses * houseSell
				c.Owner = to
				c.Houses = 0
				c.Mortg = false
			}
		}
		g.log("%s seizes %s's treasury and territory.", w.Name, p.Name)
	} else {
		for i := range g.St.Cells {
			c := &g.St.Cells[i]
			if c.Owner == pid {
				c.Owner = -1
				c.Houses = 0
				c.Mortg = false
			}
		}
	}
	p.Cash = 0
	g.checkWinner()
}

func (g *Game) checkWinner() {
	n, winner := 0, -1
	for i := range g.St.Players {
		p := &g.St.Players[i]
		if !p.Bankrupt && !p.Left && p.Name != "" {
			n++
			winner = i
		}
	}
	if n == 1 {
		g.St.Phase = "over"
		g.St.Winner = winner
		g.log("%s wins the war!", g.St.Players[winner].Name)
	}
}

func (g *Game) nextPlayer() {
	if g.St.Phase == "over" {
		return
	}
	g.St.Offer = nil
	for i := 0; i < 4; i++ {
		pid := (g.St.Current + 1 + i) % 4
		p := &g.St.Players[pid]
		if !p.Bankrupt && !p.Left && p.Name != "" {
			g.St.Current = pid
			for i := range g.St.Actors {
				a := &g.St.Actors[i]
				if a.Owner == pid && a.Alive && a.Jailed {
					a.JailTurns++
					if a.JailTurns >= jailMaxTurns {
						a.Jailed = false
						a.JailTurns = 0
						g.log("%s's unit #%d is released from the stockade.", p.Name, a.ID)
					}
				}
			}
			g.log("— %s's turn —", p.Name)
			g.St.Phase = "roll"
			return
		}
	}
	g.St.Phase = "over"
}

// ---- buy / decline / auction ----

func (g *Game) Buy() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "offer" || g.St.Offer == nil {
		return errors.New("no purchase pending")
	}
	a := g.findActor(g.St.Offer.Actor)
	if a == nil || !a.Alive || a.Owner != g.St.Current {
		return errors.New("not your unit")
	}
	c := &g.St.Cells[g.St.Offer.Cell]
	if c.Owner != -1 {
		return errors.New("already owned")
	}
	if a.Cash+g.St.Players[a.Owner].Cash < c.Buy {
		return errors.New("not enough funds (unit cash + treasury)")
	}
	g.payActor(a, c.Buy, -1)
	c.Owner = a.Owner
	g.log("%s buys (%d,%d) for $%d", g.St.Players[a.Owner].Name, c.X, c.Y, c.Buy)
	g.St.Offer = nil
	g.nextPlayer()
	return nil
}

func (g *Game) Decline() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "offer" || g.St.Offer == nil {
		return errors.New("no purchase pending")
	}
	cellID := g.St.Offer.Cell
	c := &g.St.Cells[cellID]
	g.log("%s passes on (%d,%d); it goes to auction.",
		g.St.Players[g.St.Current].Name, c.X, c.Y)
	g.St.Offer = nil
	g.St.Auction = &Auction{
		Cell:   cellID,
		Bid:    c.Buy,
		Bidder: -1,
		From:   g.St.Current,
	}
	g.St.Phase = "auction"
	return nil
}

func (g *Game) AuctionBid(amount int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "auction" || g.St.Auction == nil {
		return errors.New("no auction")
	}
	p := &g.St.Players[g.St.Current]
	if p.Bankrupt || p.Left {
		return errors.New("not your turn")
	}
	if g.auctionPassed(p.ID) {
		return errors.New("you already passed")
	}
	if amount%auctionStep != 0 {
		return errors.New("bids must be multiples of $10")
	}
	if amount < g.St.Auction.Bid+auctionStep {
		return errors.New("bid too low")
	}
	if p.Cash < amount {
		return errors.New("not enough treasury")
	}
	g.St.Auction.Bid = amount
	g.St.Auction.Bidder = p.ID
	g.St.Auction.Passed = g.auctionRemove(g.St.Auction.Passed, p.ID)
	g.log("%s bids $%d", p.Name, amount)
	g.auctionCheck()
	return nil
}

func (g *Game) AuctionPass() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "auction" || g.St.Auction == nil {
		return errors.New("no auction")
	}
	p := &g.St.Players[g.St.Current]
	if p.Bankrupt || p.Left {
		return errors.New("not your turn")
	}
	if g.auctionPassed(p.ID) {
		return errors.New("you already passed")
	}
	g.St.Auction.Passed = append(g.St.Auction.Passed, p.ID)
	g.log("%s passes", p.Name)
	g.auctionCheck()
	return nil
}

func (g *Game) auctionPassed(pid int) bool {
	for _, p := range g.St.Auction.Passed {
		if p == pid {
			return true
		}
	}
	return false
}

func (g *Game) auctionRemove(list []int, pid int) []int {
	out := list[:0]
	for _, p := range list {
		if p != pid {
			out = append(out, p)
		}
	}
	return out
}

func (g *Game) auctionAllOthersPassed(except int) bool {
	for i := range g.St.Players {
		p := &g.St.Players[i]
		if i == except || p.Bankrupt || p.Left || p.Name == "" {
			continue
		}
		if !g.auctionPassed(i) {
			return false
		}
	}
	return true
}

func (g *Game) auctionCheck() {
	a := g.St.Auction
	if a.Bidder != -1 && g.auctionAllOthersPassed(a.Bidder) {
		w := &g.St.Players[a.Bidder]
		w.Cash -= a.Bid
		c := &g.St.Cells[a.Cell]
		c.Owner = a.Bidder
		g.log("%s wins (%d,%d) at $%d", w.Name, c.X, c.Y, a.Bid)
		g.endAuction()
		return
	}
	if g.auctionAllOthersPassed(-1) {
		g.log("everyone passes; (%d,%d) stays unclaimed.",
			g.St.Cells[a.Cell].X, g.St.Cells[a.Cell].Y)
		g.endAuction()
		return
	}
	// advance to the next player who has not passed
	for i := 0; i < 4; i++ {
		pid := (g.St.Current + 1 + i) % 4
		p := &g.St.Players[pid]
		if !p.Bankrupt && !p.Left && p.Name != "" && !g.auctionPassed(pid) {
			g.St.Current = pid
			return
		}
	}
	g.endAuction()
}

func (g *Game) endAuction() {
	from := g.St.Auction.From
	g.St.Auction = nil
	g.St.Current = from
	g.nextPlayer()
}

// ---- army management (roll phase only) ----

func (g *Game) Fund(actorID, amount int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "roll" {
		return errors.New("not the roll phase")
	}
	a := g.findActor(actorID)
	if a == nil || !a.Alive || a.Owner != g.St.Current {
		return errors.New("bad unit")
	}
	if amount <= 0 || amount > g.St.Players[a.Owner].Cash {
		return errors.New("bad amount")
	}
	g.St.Players[a.Owner].Cash -= amount
	a.Cash += amount
	g.log("%s transfers $%d to unit #%d", g.St.Players[a.Owner].Name, amount, a.ID)
	return nil
}

func (g *Game) Recall(actorID int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "roll" {
		return errors.New("not the roll phase")
	}
	a := g.findActor(actorID)
	if a == nil || !a.Alive || a.Owner != g.St.Current {
		return errors.New("bad unit")
	}
	if g.aliveUnits(a.Owner) <= 1 {
		return errors.New("you must keep at least one unit")
	}
	g.St.Players[a.Owner].Cash += a.Cash
	a.Alive = false
	a.Cash = 0
	g.log("%s recalls unit #%d (its cash returns to the treasury)",
		g.St.Players[a.Owner].Name, actorID)
	return nil
}

func (g *Game) Reinforce() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "roll" {
		return errors.New("not the roll phase")
	}
	p := &g.St.Players[g.St.Current]
	if p.Bankrupt || p.Left {
		return errors.New("not your turn")
	}
	if g.aliveUnits(p.ID) >= maxActors {
		return errors.New("army is at full strength")
	}
	if p.Cash < actorCost {
		return errors.New("not enough treasury")
	}
	p.Cash -= actorCost
	g.St.Actors = append(g.St.Actors, Actor{
		ID:    g.St.NextID,
		Owner: p.ID,
		X:     p.Base % Grid,
		Y:     p.Base / Grid,
		Cash:  actorFunds,
		Alive: true,
	})
	g.log("%s recruits unit #%d for $%d", p.Name, g.St.NextID, actorCost)
	g.St.NextID++
	return nil
}

func (g *Game) Build(cellID int, sell bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "roll" {
		return errors.New("not the roll phase")
	}
	p := &g.St.Players[g.St.Current]
	if p.Bankrupt || p.Left {
		return errors.New("not your turn")
	}
	c := &g.St.Cells[cellID]
	if c.Kind != KProperty || c.Owner != p.ID {
		return errors.New("not your property")
	}
	if sell {
		if c.Houses == 0 {
			return errors.New("no houses to sell")
		}
		c.Houses--
		p.Cash += houseSell
		g.log("%s sells a house on (%d,%d) for $%d", p.Name, c.X, c.Y, houseSell)
		return nil
	}
	if c.Mortg {
		return errors.New("cell is mortgaged")
	}
	if c.Houses >= maxHouses {
		return errors.New("full")
	}
	if !g.monopoly(p.ID, c.Group) {
		return errors.New("you do not own the whole block")
	}
	if p.Cash < houseCost {
		return errors.New("not enough treasury")
	}
	p.Cash -= houseCost
	c.Houses++
	g.log("%s builds a house on (%d,%d)", p.Name, c.X, c.Y)
	return nil
}

func (g *Game) Mortgage(cellID int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "roll" {
		return errors.New("not the roll phase")
	}
	p := &g.St.Players[g.St.Current]
	if p.Bankrupt || p.Left {
		return errors.New("not your turn")
	}
	c := &g.St.Cells[cellID]
	if c.Kind != KProperty || c.Owner != p.ID || c.Mortg {
		return errors.New("bad cell")
	}
	if c.Houses > 0 {
		return errors.New("sell houses first")
	}
	c.Mortg = true
	p.Cash += c.Buy / 2
	g.log("%s mortgages (%d,%d) for $%d", p.Name, c.X, c.Y, c.Buy/2)
	return nil
}

func (g *Game) Unmortgage(cellID int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "roll" {
		return errors.New("not the roll phase")
	}
	p := &g.St.Players[g.St.Current]
	if p.Bankrupt || p.Left {
		return errors.New("not your turn")
	}
	c := &g.St.Cells[cellID]
	if c.Kind != KProperty || c.Owner != p.ID || !c.Mortg {
		return errors.New("bad cell")
	}
	cost := c.Buy * 55 / 100
	if p.Cash < cost {
		return errors.New("not enough treasury")
	}
	p.Cash -= cost
	c.Mortg = false
	g.log("%s unmortgages (%d,%d) for $%d", p.Name, c.X, c.Y, cost)
	return nil
}

func (g *Game) PayJail(actorID int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "roll" {
		return errors.New("not the roll phase")
	}
	a := g.findActor(actorID)
	if a == nil || !a.Alive || a.Owner != g.St.Current || !a.Jailed {
		return errors.New("bad unit")
	}
	p := &g.St.Players[a.Owner]
	if p.Cash < jailFine {
		return errors.New("not enough treasury")
	}
	p.Cash -= jailFine
	a.Jailed = false
	a.JailTurns = 0
	g.log("%s bails out unit #%d for $%d", p.Name, a.ID, jailFine)
	return nil
}

// ---- deals ----

func (g *Game) ProposeDeal(to int, give, want Asset) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "roll" {
		return errors.New("deals only before the roll")
	}
	p := &g.St.Players[g.St.Current]
	if p.Bankrupt || p.Left {
		return errors.New("not your turn")
	}
	if to < 0 || to >= 4 || to == p.ID {
		return errors.New("bad target")
	}
	t := &g.St.Players[to]
	if t.Bankrupt || t.Left {
		return errors.New("target is out")
	}
	if give.Cash < 0 || want.Cash < 0 {
		return errors.New("bad amount")
	}
	if p.Cash < give.Cash || t.Cash < want.Cash {
		return errors.New("cannot afford the deal")
	}
	for _, c := range give.Props {
		if g.St.Cells[c].Owner != p.ID {
			return errors.New("you do not own that cell")
		}
	}
	for _, c := range want.Props {
		if g.St.Cells[c].Owner != to {
			return errors.New("target does not own that cell")
		}
	}
	for _, c := range give.Props {
		for _, d := range want.Props {
			if c == d {
				return errors.New("overlapping cells")
			}
		}
	}
	g.St.Deals = append(g.St.Deals, Deal{
		ID:     g.St.NextDeal,
		From:   p.ID,
		To:     to,
		Give:   give,
		Want:   want,
		Status: "open",
	})
	g.St.NextDeal++
	g.log("%s offers a deal to %s", p.Name, t.Name)
	return nil
}

func (g *Game) RespondDeal(id int, accept bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "roll" {
		return errors.New("deals only before the roll")
	}
	di := -1
	for i := range g.St.Deals {
		if g.St.Deals[i].ID == id && g.St.Deals[i].Status == "open" {
			di = i
			break
		}
	}
	if di == -1 {
		return errors.New("no such deal")
	}
	d := &g.St.Deals[di]
	if d.To != g.St.Current {
		return errors.New("not your deal")
	}
	d.Status = "declined"
	if !accept {
		g.log("%s declines a deal from %s", g.St.Players[d.To].Name, g.St.Players[d.From].Name)
		g.dropDeal(di)
		return nil
	}
	f := &g.St.Players[d.From]
	t := &g.St.Players[d.To]
	if f.Cash < d.Give.Cash || t.Cash < d.Want.Cash {
		return errors.New("deal can no longer be afforded")
	}
	f.Cash += d.Want.Cash - d.Give.Cash
	t.Cash += d.Give.Cash - d.Want.Cash
	for _, c := range d.Give.Props {
		g.St.Cells[c].Owner = d.To
	}
	for _, c := range d.Want.Props {
		g.St.Cells[c].Owner = d.From
	}
	g.log("deal concluded: %s <-> %s", f.Name, t.Name)
	g.dropDeal(di)
	return nil
}

func (g *Game) dropDeal(i int) {
	g.St.Deals = append(g.St.Deals[:i], g.St.Deals[i+1:]...)
}
