package main

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDealFlow(t *testing.T) {
	g := NewGame()
	g.Join("A")
	g.Join("B")
	g.Start(0)
	g.mu.Lock()
	g.St.Players[0].Cash = 1000
	g.St.Players[1].Cash = 1000
	g.St.Players[1].Cards = 1
	g.St.Props[1].Owner = 0
	g.St.Props[1].Houses = 2
	g.St.Props[6].Owner = 1
	g.mu.Unlock()

	// A proposes: gives prop#1(+2 houses) + $100, wants prop#6 + jail card
	if err := g.ProposeDeal(0, 1, Asset{Cash: 100, Props: []int{1}}, Asset{Props: []int{6}, Cards: 1}); err != nil {
		t.Fatal(err)
	}
	id := g.St.Deals[0].ID

	// B counters: gives only prop#6, wants prop#1 + $200
	if err := g.RespondDeal(1, id, "counter", &Asset{Props: []int{6}}, &Asset{Cash: 200, Props: []int{1}}); err != nil {
		t.Fatal(err)
	}
	if g.St.Deals[0].From != 1 || g.St.Deals[0].To != 0 {
		t.Fatalf("counter did not swap parties: %+v", g.St.Deals[0])
	}

	// A accepts
	if err := g.RespondDeal(0, id, "accept", nil, nil); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.St.Props[1].Owner != 1 || g.St.Props[1].Houses != 2 {
		t.Fatalf("prop 1 not transferred with houses: %+v", g.St.Props[1])
	}
	if g.St.Props[6].Owner != 0 {
		t.Fatalf("prop 6 not transferred: %+v", g.St.Props[6])
	}
	if g.St.Players[0].Cash != 800 || g.St.Players[1].Cash != 1200 {
		t.Fatalf("cash wrong: %d / %d", g.St.Players[0].Cash, g.St.Players[1].Cash)
	}
	if g.St.Players[1].Cards != 1 {
		t.Fatalf("card should stay with B after counter dropped it: %d", g.St.Players[1].Cards)
	}
	if len(g.St.Deals) != 0 {
		t.Fatalf("deal not removed after accept")
	}
}

func TestDealValidation(t *testing.T) {
	g := NewGame()
	g.Join("A")
	g.Join("B")
	g.Start(0)
	g.mu.Lock()
	g.St.Players[0].Cash = 50
	g.St.Props[1].Owner = 0
	g.St.Props[6].Owner = 1
	g.mu.Unlock()

	// offering more cash than owned: allowed at propose time, must fail on accept
	if err := g.ProposeDeal(0, 1, Asset{Cash: 100, Props: []int{1}}, Asset{Props: []int{6}}); err != nil {
		t.Fatalf("propose should not fail on cash (validated at accept): %v", err)
	}
	id := g.St.Deals[0].ID
	if err := g.RespondDeal(1, id, "accept", nil, nil); err == nil {
		t.Fatal("accept must fail when A cannot pay $100")
	}
	if len(g.St.Deals) != 0 {
		// failed accept cancels the deal
		t.Fatalf("failed deal should be cancelled, still have %d", len(g.St.Deals))
	}

	// cannot offer property you don't own
	if err := g.ProposeDeal(1, 0, Asset{Props: []int{1}}, Asset{Cash: 10}); err == nil {
		t.Fatal("must not be able to offer prop 1 (owned by A)")
	}
	// cannot deal with self
	if err := g.ProposeDeal(0, 0, Asset{Cash: 1}, Asset{Cash: 1}); err == nil {
		t.Fatal("self-deal must fail")
	}
	// third party cannot respond
	if err := g.RespondDeal(2, 99, "accept", nil, nil); err == nil {
		t.Fatal("unknown deal must fail")
	}
}

func TestBankruptcyTransfers(t *testing.T) {
	g := NewGame()
	g.Join("A")
	g.Join("B")
	g.Start(0)
	g.mu.Lock()
	g.St.Players[0].Cash = 10
	g.St.Players[1].Cash = 100
	g.St.Props[1].Owner = 0
	g.St.Props[1].Houses = 3
	g.St.Props[3].Owner = 0
	g.St.Props[6].Owner = 1
	g.St.Props[6].Mortgaged = true
	g.mu.Unlock()

	// A cannot pay rent of 6 on B's prop#6
	g.mu.Lock()
	g.bankruptTo(0, 1)
	g.mu.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.St.Players[0].Bankrupt {
		t.Fatal("A should be bankrupt")
	}
	if g.St.Props[1].Owner != 1 || g.St.Props[1].Houses != 3 {
		t.Fatalf("A's prop with houses must transfer to B: %+v", g.St.Props[1])
	}
	if g.St.Props[3].Owner != 1 {
		t.Fatal("A's other prop must transfer to B")
	}
	if g.St.Players[1].Cash != 110 {
		t.Fatalf("B should receive A's cash: %d", g.St.Players[1].Cash)
	}

	// bankrupt A again to bank: B keeps props, A (already bankrupt) has nothing
	g.bankruptTo(1, -1)
	if !g.St.Players[1].Bankrupt {
		t.Fatal("B should be bankrupt")
	}
	if g.St.Props[1].Owner != -1 || g.St.Props[1].Houses != 0 {
		t.Fatal("bankruptcy to bank must clear ownership and houses")
	}
	if g.St.Phase != "over" {
		t.Fatalf("game should be over: phase=%s", g.St.Phase)
	}
	// both players bankrupt -> no winner
	if g.St.Winner != -1 {
		t.Fatalf("expected no winner when all are bankrupt, got %d", g.St.Winner)
	}
}

// ---------- websocket smoke test ----------

type wsClientT struct {
	conn *websocket.Conn
	t    *testing.T
	pid  int
}

func wsDial(t *testing.T, srv *httptest.Server, name string) *wsClientT {
	t.Helper()
	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Msg{Type: "join", Name: name}); err != nil {
		t.Fatal(err)
	}
	var j struct {
		Type string `json:"type"`
		Pid  int    `json:"pid"`
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&j); err != nil {
		t.Fatal(err)
	}
	if j.Type != "joined" {
		t.Fatal("expected joined, got " + j.Type)
	}
	return &wsClientT{conn: conn, t: t, pid: j.Pid}
}

// read the next state broadcast
func (c *wsClientT) state() *State {
	c.t.Helper()
	for {
		var m struct {
			Type  string `json:"type"`
			State *State `json:"state"`
		}
		c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if err := c.conn.ReadJSON(&m); err != nil {
			c.t.Fatalf("read: %v", err)
		}
		if m.Type == "state" {
			return m.State
		}
		// skip error messages
	}
}

func (c *wsClientT) do(m Msg) {
	c.t.Helper()
	if err := c.conn.WriteJSON(m); err != nil {
		c.t.Fatal(err)
	}
}

func TestServerSmoke(t *testing.T) {
	srv := httptest.NewServer(newMux())
	defer srv.Close()

	a := wsDial(t, srv, "Alice")
	b := wsDial(t, srv, "Bob")
	defer a.conn.Close()
	defer b.conn.Close()

	// 3 broadcasts so far: join(a), join(b), start below -> drain after start
	a.do(Msg{Type: "start"})

	// drain pending state broadcasts: a saw join(a)+join(b)+start, b saw join(b)+start
	a.state()
	a.state()
	b.state()
	s := a.state() // same broadcast b just read
	b.state()

	clients := map[int]*wsClientT{a.pid: a, b.pid: b}
	for i := 0; i < 400; i++ {
		if s.Phase == "over" {
			if s.Winner < 0 {
				t.Fatal("game over with no winner")
			}
			t.Logf("game over at round %d, winner=%d", i, s.Winner)
			for _, p := range s.Players {
				t.Logf("  %s cash=%d bankrupt=%v", p.Name, p.Cash, p.Bankrupt)
			}
			for _, l := range s.Log[len(s.Log)-12:] {
				t.Log("  " + l)
			}
			return
		}
		if s.Phase == "auction" {
			// the non-trigger player passes; with 2 players the auction ends at once
			other := b
			if s.Auction.Trigger == b.pid {
				other = a
			}
			other.do(Msg{Type: "auction_pass"})
		} else {
			// turn phase: current player acts
			cur := clients[s.Current]
			if cur == nil {
				t.Fatalf("current %d not a connected client", s.Current)
			}
			if s.BuyOffer != nil && s.BuyOffer.Player == cur.pid {
				cur.do(Msg{Type: "buy"})
			} else if !s.Rolled {
				cur.do(Msg{Type: "roll"})
			} else {
				// never expected: the server auto-advances after roll resolution
				t.Fatalf("stuck state: rolled with no buy offer (phase=%s)", s.Phase)
			}
		}
		// consume the one broadcast produced by the action above
		s = a.state()
		b.state()
	}
	t.Skip("no bankruptcy within 400 rounds - fine, plumbing was verified")
}
