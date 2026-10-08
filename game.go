package main

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

const (
	startCash  = 1500
	houseCost  = 100
	goPayout   = 200
	maxPlayers = 4
)

// ---------- state ----------

type Player struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Cash      int    `json:"cash"`
	Pos       int    `json:"pos"`
	InJail    bool   `json:"inJail"`
	JailTurns int    `json:"jailTurns"`
	Cards     int    `json:"cards"` // get out of jail free
	Bankrupt  bool   `json:"bankrupt"`
	Left      bool   `json:"left"`
}

type Prop struct {
	Owner     int  `json:"owner"` // -1 = unowned
	Houses    int  `json:"houses"`
	Mortgaged bool `json:"mortgaged"`
}

type Asset struct {
	Cash  int   `json:"cash"`
	Props []int `json:"props"`
	Cards int   `json:"cards"`
}

type Deal struct {
	ID      int   `json:"id"`
	From    int   `json:"from"`
	To      int   `json:"to"`
	Offer   Asset `json:"offer"` // From gives Offer, receives Request
	Request Asset `json:"request"`
}

type BuyOffer struct {
	Player int `json:"player"`
	SQ     int `json:"sq"`
	Price  int `json:"price"`
}

type Auction struct {
	SQ      int   `json:"sq"`
	Bid     int   `json:"bid"`
	Bidder  int   `json:"bidder"` // -1 = none yet
	Trigger int   `json:"trigger"`
	Passed  []int `json:"passed"`
}

type State struct {
	Phase      string     `json:"phase"` // lobby | turn | auction | over
	Players    []Player   `json:"players"`
	Current    int        `json:"current"`
	Dice       [2]int     `json:"dice"`
	Rolled     bool       `json:"rolled"`
	LastDouble bool       `json:"lastDouble"`
	Doubles    int        `json:"doubles"`
	Props      []Prop     `json:"props"`
	Board      [40]Square `json:"board"`
	BuyOffer   *BuyOffer  `json:"buyOffer"`
	Auction    *Auction   `json:"auction"`
	Deals      []Deal     `json:"deals"`
	Log        []string   `json:"log"`
	Winner     int        `json:"winner"`
	Host       int        `json:"host"`
	DeckLeft   [2]int     `json:"deckLeft"`
}

type card struct {
	text  string
	apply func(g *Game, p *Player)
}

type Game struct {
	mu        sync.Mutex
	St        *State
	rng       *rand.Rand
	chance    []card
	chest     []card
	nextDeal  int
	Broadcast func()
}

func NewGame() *Game {
	g := &Game{rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	g.St = &State{
		Phase:   "lobby",
		Props:   make([]Prop, 40),
		Current: -1,
		Winner:  -1,
		Host:    -1,
	}
	for i := range g.St.Props {
		g.St.Props[i].Owner = -1
	}
	g.St.Board = board
	g.chance = g.buildChanceDeck()
	g.chest = g.buildChestDeck()
	g.log("Welcome to Monopoly.")
	return g
}

func (g *Game) log(format string, a ...any) {
	g.St.Log = append(g.St.Log, fmt.Sprintf(format, a...))
	if len(g.St.Log) > 60 {
		g.St.Log = g.St.Log[len(g.St.Log)-60:]
	}
}

func (g *Game) alive() int {
	n := 0
	for _, p := range g.St.Players {
		if !p.Bankrupt && !p.Left {
			n++
		}
	}
	return n
}

func (g *Game) nextSurviving(from int) int {
	n := len(g.St.Players)
	for i := 1; i <= n; i++ {
		p := (from + i) % n
		if !g.St.Players[p].Bankrupt && !g.St.Players[p].Left {
			return p
		}
	}
	return -1
}

// ---------- lobby ----------

func (g *Game) Join(name string) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "lobby" {
		return -1, errors.New("game already in progress")
	}
	for i := range g.St.Players {
		if g.St.Players[i].Left {
			g.St.Players[i] = Player{ID: i, Name: name, Cash: startCash}
			if g.St.Host == -1 || g.St.Players[g.St.Host].Left {
				g.St.Host = i
			}
			g.log("%s joined.", name)
			return i, nil
		}
	}
	if len(g.St.Players) >= maxPlayers {
		return -1, errors.New("game is full (max 4 players)")
	}
	pid := len(g.St.Players)
	g.St.Players = append(g.St.Players, Player{ID: pid, Name: name, Cash: startCash})
	if g.St.Host == -1 {
		g.St.Host = pid
	}
	g.log("%s joined.", name)
	return pid, nil
}

func (g *Game) Start(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "lobby" {
		return errors.New("not in lobby")
	}
	if g.alive() < 2 {
		return errors.New("need at least 2 players to start")
	}
	g.resetForNewGame()
	g.St.Phase = "turn"
	g.St.Current = g.nextSurviving(-1)
	g.log("Game started! %s goes first.", g.St.Players[g.St.Current].Name)
	return nil
}

func (g *Game) NewGame(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase == "lobby" {
		return errors.New("game not started")
	}
	if pid != g.St.Host {
		return errors.New("only the host can start a new game")
	}
	g.resetForNewGame()
	g.St.Phase = "turn"
	g.St.Current = g.nextSurviving(-1)
	g.log("New game started! %s goes first.", g.St.Players[g.St.Current].Name)
	return nil
}

func (g *Game) resetForNewGame() {
	for i := range g.St.Players {
		p := &g.St.Players[i]
		if p.Left {
			continue
		}
		p.Cash = startCash
		p.Pos = 0
		p.InJail = false
		p.JailTurns = 0
		p.Cards = 0
		p.Bankrupt = false
	}
	for i := range g.St.Props {
		g.St.Props[i] = Prop{Owner: -1}
	}
	g.St.Deals = nil
	g.St.BuyOffer = nil
	g.St.Auction = nil
	g.St.Winner = -1
	g.St.Dice = [2]int{}
	g.chance = g.buildChanceDeck()
	g.chest = g.buildChestDeck()
	g.St.DeckLeft = [2]int{len(g.chance), len(g.chest)}
	g.St.Log = g.St.Log[:0]
}

func (g *Game) Disconnect(pid int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if pid < 0 || pid >= len(g.St.Players) {
		return
	}
	if g.St.Phase == "lobby" {
		g.St.Players[pid].Left = true
		g.St.Players[pid].Name = ""
		g.log("A player left the lobby.")
		return
	}
	p := &g.St.Players[pid]
	if !p.Bankrupt {
		g.log("%s disconnected and goes bankrupt.", p.Name)
		g.bankruptTo(pid, -1)
	}
	if g.St.Phase == "turn" && g.St.Current == pid {
		g.advanceAfterLoss(pid)
	}
}

// ---------- turn flow ----------

func (g *Game) rollCheck(pid int) error {
	if g.St.Phase != "turn" || g.St.Current != pid {
		return errors.New("not your turn")
	}
	if g.St.BuyOffer != nil && g.St.BuyOffer.Player == pid {
		return errors.New("buy or decline the property first")
	}
	return nil
}

func (g *Game) Roll(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.rollCheck(pid); err != nil {
		return err
	}
	p := &g.St.Players[pid]
	d1 := 1 + g.rng.Intn(6)
	d2 := 1 + g.rng.Intn(6)
	g.St.Dice = [2]int{d1, d2}
	g.St.Rolled = true
	g.St.LastDouble = d1 == d2
	total := d1 + d2
	g.log("%s rolled %d + %d.", p.Name, d1, d2)

	if p.InJail {
		if g.St.LastDouble {
			p.InJail = false
			p.JailTurns = 0
			g.log("%s rolls doubles and gets out of jail.", p.Name)
			g.moveAndResolve(p, total)
		} else {
			p.JailTurns++
			if p.JailTurns >= 3 {
				p.InJail = false
				p.JailTurns = 0
				g.chargeBank(p, 50)
				if !p.Bankrupt {
					g.log("%s pays $50 fine and moves.", p.Name)
					g.moveAndResolve(p, total)
				}
			} else {
				g.log("%s fails to get out of jail (attempt %d/3).", p.Name, p.JailTurns)
			}
		}
	} else {
		g.moveAndResolve(p, total)
		if !p.Bankrupt && g.St.LastDouble && g.St.BuyOffer == nil {
			g.St.Doubles++
			if g.St.Doubles >= 3 {
				g.log("%s rolls doubles three times in a row and goes to jail.", p.Name)
				g.toJail(p)
				g.St.LastDouble = false
			} else {
				g.log("%s rolls doubles and rolls again.", p.Name)
				g.St.Rolled = false
				return nil // second roll of the same turn
			}
		}
	}

	// end of turn (auto): unless the player just went bankrupt (handled in
	// bankruptTo) or must still answer a pending buy offer.
	if p.Bankrupt {
		return nil
	}
	if g.St.BuyOffer != nil && g.St.BuyOffer.Player == pid {
		return nil
	}
	g.endTurnNow(pid)
	return nil
}

func (g *Game) EndTurn(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "turn" || g.St.Current != pid {
		return errors.New("not your turn")
	}
	if g.St.BuyOffer != nil && g.St.BuyOffer.Player == pid {
		return errors.New("buy or decline the property first")
	}
	g.St.BuyOffer = nil
	g.St.Current = g.nextSurviving(pid)
	if g.St.Current == -1 {
		g.endGame()
		return nil
	}
	g.resetTurnState()
	return nil
}

func (g *Game) endTurnNow(loser int) {
	// called with lock held during the "turn" phase
	if g.St.Phase != "turn" {
		return // e.g. an auction is in progress; it advances the turn itself
	}
	if g.alive() <= 1 {
		g.endGame()
		return
	}
	g.St.Current = g.nextSurviving(loser)
	g.resetTurnState()
}

func (g *Game) resetTurnState() {
	g.St.Dice = [2]int{}
	g.St.Rolled = false
	g.St.LastDouble = false
	g.St.Doubles = 0
	g.St.BuyOffer = nil
}

func (g *Game) advanceAfterLoss(loser int) {
	// called with lock held; loser just went bankrupt
	if g.alive() <= 1 {
		g.endGame()
		return
	}
	if g.St.Phase == "turn" {
		g.endTurnNow(loser)
	}
}

func (g *Game) endGame() {
	w := -1
	for i := range g.St.Players {
		if !g.St.Players[i].Bankrupt && !g.St.Players[i].Left {
			w = i
			break
		}
	}
	g.St.Winner = w
	g.St.Phase = "over"
	g.St.Current = -1
	if w >= 0 {
		g.log("%s wins the game!", g.St.Players[w].Name)
	} else {
		g.log("Game over.")
	}
}

// ---------- movement & landing ----------

func (g *Game) moveAndResolve(p *Player, dist int) {
	start := p.Pos
	p.Pos = (start + dist) % 40
	if start != 0 && start+dist >= 40 {
		p.Cash += goPayout
		g.log("%s collects $%d for passing GO.", p.Name, goPayout)
	}
	g.resolveLanding(p)
}

func (g *Game) resolveLanding(p *Player) {
	sq := p.Pos
	s := g.St.Board[sq]
	switch s.Kind {
	case KGo, KParking, KJail:
		g.log("%s lands on square %d.", p.Name, sq)
	case KGoToJail:
		g.log("%s is sent to jail.", p.Name)
		g.toJail(p)
	case KTax:
		g.log("%s pays $%d tax.", p.Name, s.Tax)
		g.chargeBank(p, s.Tax)
	case KProperty, KRailroad, KUtility:
		pr := &g.St.Props[sq]
		if pr.Owner == -1 {
			if p.Cash >= s.Buy {
				g.St.BuyOffer = &BuyOffer{Player: p.ID, SQ: sq, Price: s.Buy}
				g.log("%s lands on unowned square %d. Buy for $%d?", p.Name, sq, s.Buy)
			} else {
				g.log("%s cannot afford square %d. Auction starts.", p.Name, sq)
				g.startAuction(sq, p.ID)
			}
		} else if pr.Owner != p.ID && !pr.Mortgaged {
			rent := g.rentAmount(sq)
			g.log("%s must pay $%d rent to %s.", p.Name, rent, g.St.Players[pr.Owner].Name)
			if p.Cash >= rent {
				p.Cash -= rent
				g.St.Players[pr.Owner].Cash += rent
			} else {
				g.log("%s cannot pay rent and goes bankrupt!", p.Name)
				g.bankruptTo(p.ID, pr.Owner)
			}
		}
	case KChance:
		g.applyCard(g.draw("Chance"), p)
	case KChest:
		g.applyCard(g.draw("Chest"), p)
	}
}

func (g *Game) toJail(p *Player) {
	p.Pos = 10
	p.InJail = true
	p.JailTurns = 0
}

func (g *Game) rentAmount(sq int) int {
	s := g.St.Board[sq]
	pr := &g.St.Props[sq]
	switch s.Kind {
	case KProperty:
		r := s.Rent[pr.Houses]
		if pr.Houses == 0 && g.ownsGroup(pr.Owner, s.Group) {
			r *= 2
		}
		return r
	case KRailroad:
		n := 0
		for i := 0; i < 40; i++ {
			if g.St.Board[i].Kind == KRailroad && g.St.Props[i].Owner == pr.Owner {
				n++
			}
		}
		return 25 << (n - 1)
	case KUtility:
		n := 0
		for i := 0; i < 40; i++ {
			if g.St.Board[i].Kind == KUtility && g.St.Props[i].Owner == pr.Owner {
				n++
			}
		}
		mult := 4
		if n == 2 {
			mult = 10
		}
		dice := g.St.Dice[0] + g.St.Dice[1]
		if dice == 0 {
			dice = 7
		}
		return mult * dice
	}
	return 0
}

func (g *Game) ownsGroup(pi, grp int) bool {
	for _, sq := range groups[grp] {
		if g.St.Props[sq].Owner != pi {
			return false
		}
	}
	return true
}

func (g *Game) chargeBank(p *Player, amount int) {
	p.Cash -= amount
	if p.Cash < 0 {
		g.log("%s cannot pay the bank and goes bankrupt!", p.Name)
		g.bankruptTo(p.ID, -1)
	}
}

// ---------- money / bankruptcy ----------

// bankruptTo transfers p's assets to creditor (player idx) or the bank (-1).
func (g *Game) bankruptTo(pi, creditor int) {
	p := &g.St.Players[pi]
	p.Bankrupt = true
	if creditor >= 0 && creditor < len(g.St.Players) {
		c := &g.St.Players[creditor]
		c.Cash += p.Cash
		c.Cards += p.Cards
		for i := range g.St.Props {
			if g.St.Props[i].Owner == pi {
				g.St.Props[i].Owner = creditor
			}
		}
		g.log("Assets of %s go to %s.", p.Name, c.Name)
	} else {
		for i := range g.St.Props {
			if g.St.Props[i].Owner == pi {
				g.St.Props[i].Owner = -1
				g.St.Props[i].Houses = 0
			}
		}
		g.log("Assets of %s go to the bank.", p.Name)
	}
	// re-evaluate a running auction (bidder may have gone bankrupt)
	if g.St.Auction != nil {
		g.finishAuctionIfDone()
	}
	// cancel deals involving the bankrupt player
	g.St.Deals = removeDealsWith(g.St.Deals, pi)
	p.Cash = 0
	p.Cards = 0
	p.InJail = false
	// the loser's turn (if any) must advance, or the game must end
	g.advanceAfterLoss(pi)
}

func removeDealsWith(deals []Deal, pid int) []Deal {
	out := deals[:0]
	for _, d := range deals {
		if d.From != pid && d.To != pid {
			out = append(out, d)
		}
	}
	return out
}

// ---------- turn actions: buy, build, mortgage ----------

func (g *Game) Buy(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	bo := g.St.BuyOffer
	if bo == nil || bo.Player != pid {
		return errors.New("no purchase offer for you")
	}
	p := &g.St.Players[pid]
	if p.Cash < bo.Price {
		return errors.New("not enough cash")
	}
	p.Cash -= bo.Price
	g.St.Props[bo.SQ].Owner = pid
	g.log("%s buys square %d for $%d.", p.Name, bo.SQ, bo.Price)
	g.St.BuyOffer = nil
	g.endTurnNow(pid)
	return nil
}

func (g *Game) Decline(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	bo := g.St.BuyOffer
	if bo == nil || bo.Player != pid {
		return errors.New("no purchase offer for you")
	}
	g.log("%s declines to buy square %d. Auction starts.", g.St.Players[pid].Name, bo.SQ)
	g.St.BuyOffer = nil
	g.startAuction(bo.SQ, pid)
	return nil
}

func (g *Game) Build(pid, sq int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "turn" || g.St.Current != pid {
		return errors.New("not your turn")
	}
	if g.St.Rolled {
		return errors.New("building is only allowed before you roll")
	}
	pr := &g.St.Props[sq]
	s := g.St.Board[sq]
	if s.Kind != KProperty || pr.Owner != pid {
		return errors.New("you don't own that property")
	}
	if pr.Mortgaged {
		return errors.New("property is mortgaged")
	}
	if pr.Houses >= 5 {
		return errors.New("already a hotel")
	}
	if !g.ownsGroup(pid, s.Group) {
		return errors.New("you don't own the full color group")
	}
	for _, p := range g.St.Players {
		if p.InJail && !p.Bankrupt && !p.Left {
			return errors.New("no building while a player is in jail")
		}
	}
	p := &g.St.Players[pid]
	if p.Cash < houseCost {
		return errors.New("not enough cash")
	}
	// even development rule
	maxH := 0
	for _, o := range groups[s.Group] {
		if g.St.Props[o].Houses > maxH {
			maxH = g.St.Props[o].Houses
		}
	}
	if pr.Houses > maxH {
		return errors.New("houses must be built evenly across the group")
	}
	p.Cash -= houseCost
	pr.Houses++
	if pr.Houses == 5 {
		g.log("%s builds a hotel on square %d.", p.Name, sq)
	} else {
		g.log("%s builds a house on square %d.", p.Name, sq)
	}
	return nil
}

func (g *Game) SellHouse(pid, sq int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "turn" || g.St.Current != pid {
		return errors.New("not your turn")
	}
	if g.St.Rolled {
		return errors.New("property changes are only allowed before you roll")
	}
	pr := &g.St.Props[sq]
	if g.St.Board[sq].Kind != KProperty || pr.Owner != pid {
		return errors.New("you don't own that property")
	}
	if pr.Houses == 0 {
		return errors.New("no houses to sell")
	}
	p := &g.St.Players[pid]
	pr.Houses--
	p.Cash += houseCost / 2
	g.log("%s sells a house on square %d for $%d.", p.Name, sq, houseCost/2)
	return nil
}

func mortValue(buy int) int {
	v := buy / 2
	return v - v%5
}

func (g *Game) Mortgage(pid, sq int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "turn" || g.St.Current != pid {
		return errors.New("not your turn")
	}
	if g.St.Rolled {
		return errors.New("property changes are only allowed before you roll")
	}
	pr := &g.St.Props[sq]
	s := g.St.Board[sq]
	if s.Kind != KProperty && s.Kind != KRailroad && s.Kind != KUtility {
		return errors.New("not a mortgageable square")
	}
	if pr.Owner != pid {
		return errors.New("you don't own that property")
	}
	if pr.Mortgaged {
		return errors.New("already mortgaged")
	}
	if pr.Houses > 0 {
		return errors.New("sell the houses first")
	}
	v := mortValue(s.Buy)
	g.St.Players[pid].Cash += v
	pr.Mortgaged = true
	g.log("%s mortgages square %d for $%d.", g.St.Players[pid].Name, sq, v)
	return nil
}

func (g *Game) Unmortgage(pid, sq int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "turn" || g.St.Current != pid {
		return errors.New("not your turn")
	}
	if g.St.Rolled {
		return errors.New("property changes are only allowed before you roll")
	}
	pr := &g.St.Props[sq]
	s := g.St.Board[sq]
	if pr.Owner != pid || !pr.Mortgaged {
		return errors.New("not your mortgaged property")
	}
	v := int(float64(mortValue(s.Buy)) * 1.1)
	p := &g.St.Players[pid]
	if p.Cash < v {
		return errors.New("not enough cash")
	}
	p.Cash -= v
	pr.Mortgaged = false
	g.log("%s unmortgages square %d for $%d.", p.Name, sq, v)
	return nil
}

// ---------- jail ----------

func (g *Game) PayJail(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "turn" || g.St.Current != pid {
		return errors.New("not your turn")
	}
	p := &g.St.Players[pid]
	if !p.InJail {
		return errors.New("not in jail")
	}
	if g.St.Rolled {
		return errors.New("already rolled this turn")
	}
	if p.Cash < 50 {
		return errors.New("not enough cash")
	}
	p.Cash -= 50
	p.InJail = false
	p.JailTurns = 0
	g.log("%s pays $50 to get out of jail.", p.Name)
	return nil
}

func (g *Game) UseCard(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "turn" || g.St.Current != pid {
		return errors.New("not your turn")
	}
	p := &g.St.Players[pid]
	if !p.InJail {
		return errors.New("not in jail")
	}
	if g.St.Rolled {
		return errors.New("already rolled this turn")
	}
	if p.Cards == 0 {
		return errors.New("no get-out-of-jail cards")
	}
	p.Cards--
	p.InJail = false
	p.JailTurns = 0
	g.log("%s uses a get-out-of-jail card.", p.Name)
	return nil
}

// ---------- auction ----------

func (g *Game) startAuction(sq, trigger int) {
	g.St.Auction = &Auction{SQ: sq, Bid: 0, Bidder: -1, Trigger: trigger, Passed: []int{}}
	g.St.Phase = "auction"
	g.log("Auction for square %d (price $%d).", sq, g.St.Board[sq].Buy)
}

func (g *Game) eligibleBidders() []int {
	var out []int
	a := g.St.Auction
	for i, p := range g.St.Players {
		if i == a.Trigger || p.Bankrupt || p.Left {
			continue
		}
		out = append(out, i)
	}
	return out
}

func (g *Game) AuctionBid(pid, amount int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "auction" {
		return errors.New("no auction in progress")
	}
	a := g.St.Auction
	if pid == a.Trigger || g.St.Players[pid].Bankrupt || g.St.Players[pid].Left {
		return errors.New("you can't bid")
	}
	for _, p := range a.Passed {
		if p == pid {
			return errors.New("you already passed")
		}
	}
	if amount < 0 {
		return errors.New("bad amount")
	}
	if a.Bidder != -1 && amount <= a.Bid {
		return fmt.Errorf("bid must be higher than current $%d", a.Bid)
	}
	a.Bid = amount
	a.Bidder = pid
	g.log("%s bids $%d on square %d.", g.St.Players[pid].Name, amount, a.SQ)
	g.finishAuctionIfDone()
	return nil
}

func (g *Game) AuctionPass(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "auction" {
		return errors.New("no auction in progress")
	}
	a := g.St.Auction
	if pid == a.Trigger || g.St.Players[pid].Bankrupt || g.St.Players[pid].Left {
		return errors.New("you can't pass")
	}
	for _, p := range a.Passed {
		if p == pid {
			return errors.New("already passed")
		}
	}
	a.Passed = append(a.Passed, pid)
	g.log("%s passes on the auction.", g.St.Players[pid].Name)
	g.finishAuctionIfDone()
	return nil
}

func (g *Game) finishAuctionIfDone() {
	a := g.St.Auction
	if a == nil {
		return
	}
	eligible := g.eligibleBidders()
	if a.Bidder == -1 {
		if len(eligible) == len(a.Passed) {
			g.awardAuction()
		}
		return
	}
	// all non-bidders must have passed
	allPassed := true
	for _, i := range eligible {
		if i == a.Bidder {
			continue
		}
		passed := false
		for _, p := range a.Passed {
			if p == i {
				passed = true
				break
			}
		}
		if !passed {
			allPassed = false
			break
		}
	}
	if allPassed {
		g.awardAuction()
	}
}

func (g *Game) awardAuction() {
	a := g.St.Auction
	if a.Bidder >= 0 && (g.St.Players[a.Bidder].Bankrupt || g.St.Players[a.Bidder].Left) {
		g.log("Square %d is not sold (bidder out of the game).", a.SQ)
		g.finishAuction(a.Trigger)
		return
	}
	if a.Bidder >= 0 && a.Bid > 0 {
		w := &g.St.Players[a.Bidder]
		if a.Bid > w.Cash {
			g.log("%s wins the auction but cannot pay and goes bankrupt!", w.Name)
			g.St.Props[a.SQ].Owner = -1
			g.bankruptTo(a.Bidder, -1)
		} else {
			w.Cash -= a.Bid
			g.St.Props[a.SQ].Owner = a.Bidder
			g.log("%s wins square %d for $%d.", w.Name, a.SQ, a.Bid)
		}
	} else {
		g.log("Square %d is not sold.", a.SQ)
	}
	g.finishAuction(a.Trigger)
}

// finishAuction closes the auction and advances the turn: the auction was
// triggered by the current player, whose turn ends with it.
func (g *Game) finishAuction(trigger int) {
	g.St.Auction = nil
	g.St.Phase = "turn"
	if g.alive() <= 1 {
		g.endGame()
		return
	}
	g.St.Current = g.nextSurviving(trigger)
	g.resetTurnState()
}

// ---------- deals ----------

func (g *Game) ProposeDeal(pid, to int, offer, request Asset) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Phase != "turn" || g.St.Current != pid {
		return errors.New("you can only propose deals at the start of your own turn")
	}
	if g.St.Rolled {
		return errors.New("you already rolled this turn - deals must come before the roll")
	}
	if to == pid {
		return errors.New("can't deal with yourself")
	}
	if to < 0 || to >= len(g.St.Players) {
		return errors.New("unknown player")
	}
	from := &g.St.Players[pid]
	other := &g.St.Players[to]
	if from.Bankrupt || from.Left || other.Bankrupt || other.Left {
		return errors.New("invalid deal participant")
	}
	if offer.Cash < 0 || request.Cash < 0 || offer.Cards < 0 || request.Cards < 0 {
		return errors.New("negative values not allowed")
	}
	if offer.Cards > from.Cards {
		return errors.New("not enough cards")
	}
	if !g.ownsAll(from, offer.Props) {
		return errors.New("you don't own all the properties offered")
	}
	if !uniqueProps(request.Props) {
		return errors.New("duplicate properties requested")
	}
	g.nextDeal++
	g.St.Deals = append(g.St.Deals, Deal{
		ID: g.nextDeal, From: pid, To: to, Offer: offer, Request: request,
	})
	g.log("%s proposes a deal to %s.", from.Name, other.Name)
	return nil
}

func uniqueProps(props []int) bool {
	seen := map[int]bool{}
	for _, p := range props {
		if p < 0 || p >= 40 {
			return false
		}
		if seen[p] {
			return false
		}
		seen[p] = true
	}
	return true
}

func (g *Game) ownsAll(p *Player, props []int) bool {
	if !uniqueProps(props) {
		return false
	}
	for _, sq := range props {
		if g.St.Props[sq].Owner != p.ID {
			return false
		}
	}
	return true
}

func (g *Game) findDeal(id int) *Deal {
	for i := range g.St.Deals {
		if g.St.Deals[i].ID == id {
			return &g.St.Deals[i]
		}
	}
	return nil
}

func (g *Game) RespondDeal(pid, dealID int, action string, offer, request *Asset) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	d := g.findDeal(dealID)
	if d == nil {
		return errors.New("deal not found")
	}
	if d.From != pid && d.To != pid {
		return errors.New("not your deal")
	}
	switch action {
	case "decline":
		g.log("%s declines deal #%d.", g.St.Players[pid].Name, dealID)
		g.removeDeal(dealID)
	case "accept":
		if err := g.applyDeal(d); err != nil {
			g.log("Deal #%d failed: %v. Deal cancelled.", dealID, err)
			g.removeDeal(dealID)
			return err
		}
		g.log("Deal #%d accepted.", dealID)
		g.removeDeal(dealID)
	case "counter":
		if offer == nil || request == nil {
			return errors.New("counter needs both offer and request")
		}
		other := d.From
		if d.From == pid {
			other = d.To
		}
		me := &g.St.Players[pid]
		if offer.Cash < 0 || request.Cash < 0 || offer.Cards < 0 || request.Cards < 0 {
			return errors.New("negative values not allowed")
		}
		if offer.Cards > me.Cards {
			return errors.New("not enough cards")
		}
		if !g.ownsAll(me, offer.Props) {
			return errors.New("you don't own all the properties offered")
		}
		if !uniqueProps(request.Props) {
			return errors.New("duplicate properties requested")
		}
		d.From = pid
		d.To = other
		d.Offer = *offer
		d.Request = *request
		g.log("%s counters deal #%d.", me.Name, dealID)
	default:
		return errors.New("unknown action")
	}
	return nil
}

func (g *Game) removeDeal(id int) {
	for i := range g.St.Deals {
		if g.St.Deals[i].ID == id {
			g.St.Deals = append(g.St.Deals[:i], g.St.Deals[i+1:]...)
			return
		}
	}
}

func (g *Game) applyDeal(d *Deal) error {
	from := &g.St.Players[d.From]
	to := &g.St.Players[d.To]
	if from.Bankrupt || from.Left || to.Bankrupt || to.Left {
		return errors.New("a deal participant is no longer in the game")
	}
	off, req := d.Offer, d.Request
	if off.Cash > from.Cash || req.Cash > to.Cash {
		return errors.New("insufficient cash")
	}
	if off.Cards > from.Cards || req.Cards > to.Cards {
		return errors.New("insufficient cards")
	}
	if !g.ownsAll(from, off.Props) {
		return errors.New("offered properties changed ownership")
	}
	if !g.ownsAll(to, req.Props) {
		return errors.New("requested properties changed ownership")
	}
	from.Cash += req.Cash - off.Cash
	to.Cash += off.Cash - req.Cash
	from.Cards += req.Cards - off.Cards
	to.Cards += off.Cards - req.Cards
	for _, sq := range off.Props {
		g.St.Props[sq].Owner = d.To
	}
	for _, sq := range req.Props {
		g.St.Props[sq].Owner = d.From
	}
	g.log("%s and %s complete a deal.", from.Name, to.Name)
	return nil
}

// ---------- cards ----------

func (g *Game) buildChanceDeck() []card {
	c := []card{
		{"Advance to GO.", func(g *Game, p *Player) {
			start := p.Pos
			p.Pos = 0
			if start != 0 {
				p.Cash += goPayout
				g.log("%s collects $%d passing GO.", p.Name, goPayout)
			}
		}},
		{"Advance to the nearest railroad. Pay twice the fare.", func(g *Game, p *Player) {
			g.advanceToTargets(p, []int{5, 14, 25, 38})
			fare := 25
			n := 0
			for i := 0; i < 40; i++ {
				if g.St.Board[i].Kind == KRailroad && g.St.Props[i].Owner == p.ID {
					n++
				}
			}
			if n > 0 {
				fare = 25 << (n - 1)
			}
			g.log("%s pays $%d rail fare.", p.Name, fare*2)
			g.chargeBank(p, fare*2)
		}},
		{"Advance to the nearest utility. Pay 10 times your roll.", func(g *Game, p *Player) {
			g.advanceToTargets(p, []int{12, 28})
			dice := g.St.Dice[0] + g.St.Dice[1]
			if dice == 0 {
				dice = 7
			}
			g.log("%s pays $%d to the utility company.", p.Name, dice*10)
			g.chargeBank(p, dice*10)
		}},
		{"The bank pays you $50.", func(g *Game, p *Player) { p.Cash += 50 }},
		{"Get Out of Jail Free.", func(g *Game, p *Player) {
			p.Cards++
			if p.InJail {
				p.InJail = false
				p.JailTurns = 0
				g.log("%s gets out of jail.", p.Name)
			}
		}},
		{"Go directly to jail.", func(g *Game, p *Player) { g.toJail(p) }},
		{"Go back 3 spaces.", func(g *Game, p *Player) { p.Pos = (p.Pos + 37) % 40 }},
		{"Income tax refund. The bank pays you $20.", func(g *Game, p *Player) { p.Cash += 20 }},
		{"Make house repairs: $25 per house, $110 per hotel.", func(g *Game, p *Player) {
			total := 0
			for i := range g.St.Props {
				if g.St.Props[i].Owner == p.ID {
					h := g.St.Props[i].Houses
					if h == 5 {
						total += 110
					} else {
						total += h * 25
					}
				}
			}
			if total > 0 {
				g.log("%s pays $%d in repairs.", p.Name, total)
				g.chargeBank(p, total)
			}
		}},
		{"Advance to square 11.", func(g *Game, p *Player) {
			g.advanceToSquare(p, 11)
		}},
		{"Speeding fine. Pay $15.", func(g *Game, p *Player) { g.chargeBank(p, 15) }},
		{"You are elected chairman. Pay each other player $50.", func(g *Game, p *Player) {
			total := 0
			for i := range g.St.Players {
				if i != p.ID && !g.St.Players[i].Bankrupt && !g.St.Players[i].Left {
					g.St.Players[i].Cash += 50
					total += 50
				}
			}
			if total > 0 {
				g.chargeBank(p, total)
			}
		}},
		{"Your building loan matures. The bank pays you $150.", func(g *Game, p *Player) { p.Cash += 150 }},
		{"Doctor's fee. Pay $50.", func(g *Game, p *Player) { g.chargeBank(p, 50) }},
		{"Second prize in a beauty contest. The bank pays you $10.", func(g *Game, p *Player) { p.Cash += 10 }},
		{"You inherit $100 from a distant relative.", func(g *Game, p *Player) { p.Cash += 100 }},
	}
	g.shuffle(c)
	return c
}

func (g *Game) buildChestDeck() []card {
	c := []card{
		{"Advance to GO.", func(g *Game, p *Player) {
			start := p.Pos
			p.Pos = 0
			if start != 0 {
				p.Cash += goPayout
				g.log("%s collects $%d passing GO.", p.Name, goPayout)
			}
		}},
		{"Bank error in your favor. The bank pays you $200.", func(g *Game, p *Player) { p.Cash += 200 }},
		{"Doctor's fee. Pay $50.", func(g *Game, p *Player) { g.chargeBank(p, 50) }},
		{"Go back 3 spaces.", func(g *Game, p *Player) { p.Pos = (p.Pos + 37) % 40 }},
		{"Get Out of Jail Free.", func(g *Game, p *Player) {
			p.Cards++
			if p.InJail {
				p.InJail = false
				p.JailTurns = 0
				g.log("%s gets out of jail.", p.Name)
			}
		}},
		{"Income tax. Pay $200.", func(g *Game, p *Player) { g.chargeBank(p, 200) }},
		{"You inherit $100.", func(g *Game, p *Player) { p.Cash += 100 }},
		{"Life insurance matures. The bank pays you $100.", func(g *Game, p *Player) { p.Cash += 100 }},
		{"Medicine. Pay $50.", func(g *Game, p *Player) { g.chargeBank(p, 50) }},
		{"School fees. Pay $15.", func(g *Game, p *Player) { g.chargeBank(p, 15) }},
		{"You found a stray check. The bank pays you $100.", func(g *Game, p *Player) { p.Cash += 100 }},
		{"Wedding. Pay $50.", func(g *Game, p *Player) { g.chargeBank(p, 50) }},
		{"You won a crossword prize. The bank pays you $100.", func(g *Game, p *Player) { p.Cash += 100 }},
		{"Birthday. The bank pays you $10.", func(g *Game, p *Player) { p.Cash += 10 }},
		{"Consultancy fees. The bank pays you $10.", func(g *Game, p *Player) { p.Cash += 10 }},
		{"Income tax refund. The bank pays you $20.", func(g *Game, p *Player) { p.Cash += 20 }},
	}
	g.shuffle(c)
	return c
}

func (g *Game) shuffle(c []card) {
	g.rng.Shuffle(len(c), func(i, j int) { c[i], c[j] = c[j], c[i] })
}

func (g *Game) draw(which string) card {
	if which == "Chance" {
		if len(g.chance) == 0 {
			g.chance = g.buildChanceDeck()
		}
		c := g.chance[len(g.chance)-1]
		g.chance = g.chance[:len(g.chance)-1]
		g.St.DeckLeft[0] = len(g.chance)
		return c
	}
	if len(g.chest) == 0 {
		g.chest = g.buildChestDeck()
	}
	c := g.chest[len(g.chest)-1]
	g.chest = g.chest[:len(g.chest)-1]
	g.St.DeckLeft[1] = len(g.chest)
	return c
}

func (g *Game) advanceToTargets(p *Player, targets []int) {
	best, bestDist := -1, 100
	for _, t := range targets {
		d := (t - p.Pos + 40) % 40
		if d == 0 {
			d = 40
		}
		if d < bestDist {
			best, bestDist = t, d
		}
	}
	if best >= 0 {
		start := p.Pos
		p.Pos = (start + bestDist) % 40
		if start+bestDist >= 40 {
			p.Cash += goPayout
			g.log("%s collects $%d passing GO.", p.Name, goPayout)
		}
	}
}

func (g *Game) advanceToSquare(p *Player, target int) {
	d := (target - p.Pos + 40) % 40
	if d == 0 {
		d = 40
	}
	start := p.Pos
	p.Pos = (start + d) % 40
	if start+d >= 40 {
		p.Cash += goPayout
		g.log("%s collects $%d passing GO.", p.Name, goPayout)
	}
}

func (g *Game) applyCard(c card, p *Player) {
	g.log("%s draws: %s", p.Name, c.text)
	c.apply(g, p)
}
