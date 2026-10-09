package main

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- bounce ----

func TestBounce(t *testing.T) {
	// 3 -> 2,1,0,1 (bounces at the left wall); 4+12=16 lands exactly on 0
	cases := []struct{ c, steps, want int }{
		{0, 0, 0}, {0, 8, 8}, {0, 9, 7}, {8, 1, 7}, {8, 9, 1},
		{3, 4, 7}, {3, -4, 1}, {4, 12, 0},
	}
	for _, tc := range cases {
		if got := Bounce1d(tc.c, tc.steps); got != tc.want {
			t.Errorf("Bounce1d(%d, %d) = %d, want %d", tc.c, tc.steps, got, tc.want)
		}
	}
	// property: bouncing by an even multiple of 16 returns to start
	for c := 0; c < Grid; c++ {
		if got := Bounce1d(c, 16*3); got != c {
			t.Errorf("Bounce1d(%d, 48) = %d, want %d", c, got, c)
		}
	}
}

// ---- map sanity ----

func TestMapLayout(t *testing.T) {
	cells := NewCells()
	var kindCount = map[Kind]int{}
	groups := map[int]int{}
	for i, c := range cells {
		if c.X != i%Grid || c.Y != i/Grid {
			t.Fatalf("cell %d coords wrong: %d,%d", i, c.X, c.Y)
		}
		kindCount[c.Kind]++
		if c.Kind == KProperty {
			groups[c.Group]++
			if c.Buy == 0 {
				t.Fatalf("cell %d has no price", i)
			}
		}
	}
	if kindCount[KBase] != 4 {
		t.Fatalf("bases: %d", kindCount[KBase])
	}
	if kindCount[KHighway] != 4 {
		t.Fatalf("highways: %d", kindCount[KHighway])
	}
	if kindCount[KBank] != 1 || kindCount[KJail] != 1 || kindCount[KTax] != 4 {
		t.Fatalf("specials: bank=%d jail=%d tax=%d", kindCount[KBank], kindCount[KJail], kindCount[KTax])
	}
	if kindCount[KChance]+kindCount[KChest]+kindCount[KParking] < 20 {
		t.Fatalf("too few event cells: %+v", kindCount)
	}
	for g := 0; g < 8; g++ {
		if groups[g] != 4 {
			t.Fatalf("group %d has %d cells, want 4", g, groups[g])
		}
	}
}

// ---- setup helpers ----

func newTestGame(t *testing.T, names ...string) *Game {
	t.Helper()
	g := NewGame()
	for _, n := range names {
		if _, err := g.Join(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.Start(); err != nil {
		t.Fatal(err)
	}
	return g
}

func firstActor(g *Game, pid int) *Actor {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := range g.St.Actors {
		if g.St.Actors[i].Owner == pid && g.St.Actors[i].Alive {
			return &g.St.Actors[i]
		}
	}
	return nil
}

func actorAt(g *Game, x, y int, pid int) *Actor {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := range g.St.Actors {
		a := &g.St.Actors[i]
		if a.Alive && a.Owner == pid && a.X == x && a.Y == y {
			return a
		}
	}
	return nil
}

// ---- movement & rent ----

func TestMoveAndBuy(t *testing.T) {
	g := newTestGame(t, "A", "B")
	// roll to a known state
	g.St.Dice = [2]int{1, 1}
	g.mu.Lock()
	g.St.Phase = "move"
	g.mu.Unlock()
	// A's actor 0 at base (0,8): move east by 2 -> (2,8) which is chance '?'...
	// (0,8)=A base, (1,8)='!', (2,8)='?' chest. Let's pick a deterministic property:
	// base B at (8,8); (6,8) is '?'... use a property cell: (1,7) is group a.
	// From (0,8) go south? (0,7)='!'... simpler: move B's unit west by 2 from (8,8) -> (6,8) '?'
	// Instead test directly: place a unit on a property via move from (3,8)?
	// Easiest deterministic: A unit at (0,8), move east 3 -> (3,8)='!' chance.
	// So: move east 1 -> (1,8) chance. Let's just verify positions with the bounce math:
	a := g.findActorForTest(0)
	g.Move(0, 1, 0) // dice 1+1=2: (0,8) -> (2,8)
	g.mu.Lock()
	if a.X != 2 || a.Y != 8 {
		t.Fatalf("expected (2,8), got (%d,%d)", a.X, a.Y)
	}
	// (2,8) is a chest -> some card effect; ensure game state is consistent
	if g.St.Phase == "move" {
		t.Fatal("phase should have advanced")
	}
	g.mu.Unlock()
}

func (g *Game) findActorForTest(id int) *Actor {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.findActor(id)
}

func TestRentAndMonopoly(t *testing.T) {
	g := newTestGame(t, "A", "B")
	// A owns whole group a: cells (1,6),(2,6),(1,7),(2,7) = ids 6*9+1=55, 6*9+2=56, 7*9+1=64, 7*9+2=65
	for _, id := range []int{55, 56, 64, 65} {
		g.mu.Lock()
		g.St.Cells[id].Owner = 0
		g.St.Cells[id].Houses = 0
		g.mu.Unlock()
	}
	// B's unit at (3,8) (chance? (3,8)='!'). Place it manually at (2,8)='?' ...
	// put B unit directly adjacent and charge:
	g.mu.Lock()
	b := &g.St.Actors[3] // first B actor
	b.X, b.Y = 2, 6 // on group-a cell (2,6), owner A
	b.Cash = 500
	g.St.Players[0].Cash = 0
	g.mu.Unlock()
	// charge rent directly (as resolveLanding would): cell (2,6) = id 56, group a rent0=20, monopoly x2
	g.mu.Lock()
	g.charge(b, g.St.Cells[56].Rent[0]*2, 0) // 20*2=40
	g.mu.Unlock()
	g.mu.Lock()
	if g.St.Players[0].Cash != 40 {
		t.Fatalf("A should have 40, got %d", g.St.Players[0].Cash)
	}
	if b.Cash != 460 {
		t.Fatalf("unit should have 460, got %d", b.Cash)
	}
	g.mu.Unlock()
}

// ---- capture & elimination ----

func TestCaptureAndElimination(t *testing.T) {
	g := newTestGame(t, "A", "B")
	g.mu.Lock()
	// A's unit 0 is broke; B's unit 3 is on the same cell
	a := &g.St.Actors[0]
	b := &g.St.Actors[3]
	a.Cash = 0
	b.X, b.Y = a.X, a.Y
	g.St.Players[0].Cash = 0
	g.mu.Unlock()

	g.mu.Lock()
	g.charge(a, 100, 1) // can't pay -> captured, B gets the loot (0), A eliminated (3 units! wait A has 3)
	g.mu.Unlock()
	g.mu.Lock()
	if a.Alive {
		t.Fatal("unit 0 should be captured")
	}
	// A still has 2 units -> not eliminated
	if g.St.Players[0].Bankrupt {
		t.Fatal("A should not be bankrupt (has other units)")
	}
	g.mu.Unlock()

	// now kill A's remaining units -> eliminated
	g.mu.Lock()
	for i := 1; i < 3; i++ {
		ua := &g.St.Actors[i]
		ua.Cash = 0
		g.capture(ua, 1)
	}
	if !g.St.Players[0].Bankrupt {
		t.Fatal("A should be bankrupt after losing all units")
	}
	if g.St.Phase != "over" {
		t.Fatalf("game should be over, phase=%s", g.St.Phase)
	}
	if g.St.Winner != 1 {
		t.Fatalf("B should win, winner=%d", g.St.Winner)
	}
	g.mu.Unlock()
}

// ---- deals ----

func TestDeal2D(t *testing.T) {
	g := newTestGame(t, "A", "B")
	g.mu.Lock()
	g.St.Cells[55].Owner = 0 // A owns a group-a cell
	g.St.Players[0].Cash = 100
	g.St.Players[1].Cash = 100
	g.St.Current = 0
	g.St.Phase = "roll"
	g.mu.Unlock()
	err := g.ProposeDeal(1,
		Asset{Cash: 30, Props: []int{55}},
		Asset{Cash: 10})
	if err != nil {
		t.Fatal(err)
	}
	// B accepts
	g.mu.Lock()
	g.St.Current = 1
	g.mu.Unlock()
	if err := g.RespondDeal(0, true); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	if g.St.Players[0].Cash != 80 {
		t.Fatalf("A cash 80, got %d", g.St.Players[0].Cash)
	}
	if g.St.Players[1].Cash != 120 {
		t.Fatalf("B cash 120, got %d", g.St.Players[1].Cash)
	}
	if g.St.Cells[55].Owner != 1 {
		t.Fatal("cell 55 should belong to B")
	}
	if len(g.St.Deals) != 0 {
		t.Fatal("deal should be dropped")
	}
	g.mu.Unlock()
}

// ---- auction ----

func TestAuction2D(t *testing.T) {
	g := newTestGame(t, "A", "B")
	// A passes on a property -> auction; B bids and wins (A passes)
	g.mu.Lock()
	g.St.Offer = &BuyOffer{Actor: 0, Cell: 55}
	g.St.Phase = "offer"
	g.St.Current = 0
	g.mu.Unlock()
	if err := g.Decline(); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	if g.St.Phase != "auction" || g.St.Auction.Bid != g.St.Cells[55].Buy {
		t.Fatalf("auction should start at buy price, got %+v", g.St.Auction)
	}
	// current is A (0): A passes
	g.mu.Unlock()
	if err := g.AuctionPass(); err != nil {
		t.Fatal(err)
	}
	// now B bids
	if err := g.AuctionBid(120); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	// A passed, B bidding -> B wins immediately (all others passed)
	if g.St.Cells[55].Owner != 1 {
		t.Fatalf("B should own 55, owner=%d phase=%s", g.St.Cells[55].Owner, g.St.Phase)
	}
	if g.St.Players[1].Cash != startTreasury-120 {
		t.Fatalf("B treasury, got %d", g.St.Players[1].Cash)
	}
	g.mu.Unlock()
}

// ---- self-play: the whole game must terminate ----

func TestSelfPlay2D(t *testing.T) {
	for seed := int64(0); seed < 60; seed++ {
		rng := rand.New(rand.NewSource(seed))
		g := newTestGame(t, "A", "B", "C")
		g.mu.Lock()
		g.rng = rand.New(rand.NewSource(seed + 100000)) // deterministic dice & cards
		g.mu.Unlock()
		steps := 0
		for g.St.Phase != "over" && steps < 20000 {
			steps++
			cur := g.St.Current
			switch g.St.Phase {
			case "roll":
				// occasionally manage the army
				if rng.Intn(10) == 0 {
					g.Reinforce()
				}
				if rng.Intn(8) == 0 {
					// build on a random owned property
					g.mu.Lock()
					var mine []int
					for i := range g.St.Cells {
						if g.St.Cells[i].Owner == cur && g.St.Cells[i].Kind == KProperty {
							mine = append(mine, i)
						}
					}
					g.mu.Unlock()
					if len(mine) > 0 {
						g.Build(mine[rng.Intn(len(mine))], false)
					}
				}
				if err := g.Roll(); err != nil {
					t.Fatalf("seed %d step %d: roll: %v", seed, steps, err)
				}
			case "move":
				g.mu.Lock()
				var movable []*Actor
				var diceSum int
				diceSum = g.St.Dice[0] + g.St.Dice[1]
				for i := range g.St.Actors {
					a := &g.St.Actors[i]
					if a.Owner == cur && a.Alive && !a.Jailed {
						movable = append(movable, a)
					}
				}
				g.mu.Unlock()
				if len(movable) == 0 {
					t.Fatalf("seed %d: no movable actors in move phase", seed)
				}
				a := movable[rng.Intn(len(movable))]
				dirs := [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
				d := dirs[rng.Intn(8)]
				g.mu.Lock()
				aid := a.ID
				g.mu.Unlock()
				if err := g.Move(aid, d[0], d[1]); err != nil {
					t.Fatalf("seed %d step %d: move: %v (dice sum %d)", seed, steps, err, diceSum)
				}
			case "offer":
				g.mu.Lock()
				afford := false
				if a := g.findActor(g.St.Offer.Actor); a != nil {
					afford = a.Cash+g.St.Players[a.Owner].Cash >= g.St.Cells[g.St.Offer.Cell].Buy
				}
				g.mu.Unlock()
				if afford && rng.Intn(2) == 0 {
					if err := g.Buy(); err != nil {
						t.Fatalf("seed %d: buy: %v", seed, err)
					}
				} else {
					if err := g.Decline(); err != nil {
						t.Fatalf("seed %d: decline: %v", seed, err)
					}
				}
			case "auction":
				g.mu.Lock()
				bidder := g.St.Current
				cash := g.St.Players[bidder].Cash
				min := g.St.Auction.Bid + auctionStep
				g.mu.Unlock()
				if cash >= min+40 && rng.Intn(3) == 0 {
					if err := g.AuctionBid(min + rng.Intn(4)*auctionStep); err != nil {
						t.Fatalf("seed %d: bid: %v", seed, err)
					}
				} else {
					if err := g.AuctionPass(); err != nil {
						t.Fatalf("seed %d: pass: %v", seed, err)
					}
				}
			default:
				t.Fatalf("seed %d: unexpected phase %q", seed, g.St.Phase)
			}
			// invariants
			g.mu.Lock()
			for i := range g.St.Players {
				p := &g.St.Players[i]
				if p.Cash < 0 {
					t.Fatalf("seed %d: negative treasury %d", seed, p.Cash)
				}
				if p.Bankrupt && g.aliveUnits(i) > 0 {
					t.Fatalf("seed %d: bankrupt player %d has living units", seed, i)
				}
			}
			for i := range g.St.Cells {
				c := &g.St.Cells[i]
				if c.Owner >= 0 && (g.St.Players[c.Owner].Bankrupt || g.St.Players[c.Owner].Left) {
					t.Fatalf("seed %d: cell %d owned by eliminated player %d", seed, i, c.Owner)
				}
			}
			g.mu.Unlock()
		}
		if g.St.Phase != "over" {
			t.Fatalf("seed %d: game did not finish in %d steps", seed, steps)
		}
		g.mu.Lock()
		w := g.St.Winner
		g.mu.Unlock()
		if w < 0 || g.St.Players[w].Bankrupt {
			t.Fatalf("seed %d: bad winner %d", seed, w)
		}
	}
}

// ---- host removal / lobby ----

func TestRemoveAndRejoin2D(t *testing.T) {
	g := NewGame()
	for _, n := range []string{"A", "B"} {
		g.Join(n)
	}
	if err := g.RemovePlayer(0, 1); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	if !g.St.Players[1].Left {
		t.Fatal("player should be left")
	}
	g.mu.Unlock()
	// a new player can take the freed slot
	pid, err := g.Join("C")
	if err != nil {
		t.Fatal(err)
	}
	if pid != 1 {
		t.Fatalf("expected slot 1, got %d", pid)
	}
}

// ---- server smoke ----

func TestServer2D(t *testing.T) {
	ts := httptest.NewServer(newMux(NewHub(NewGame())))
	defer ts.Close()
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
	}
}
