package main

import (
	"fmt"
	"testing"
)

// aiStep performs one plausible action for the current situation.
func aiStep(g *Game) {
	st := g.St
	// auction first
	if st.Phase == "auction" && st.Auction != nil {
		a := st.Auction
		for i, p := range st.Players {
			if i == a.Trigger || p.Bankrupt || p.Left {
				continue
			}
			if a.Bidder == i {
				continue
			}
			passed := false
			for _, q := range a.Passed {
				if q == i {
					passed = true
				}
			}
			if passed {
				continue
			}
			minBid := a.Bid
			if a.Bidder != -1 {
				minBid = a.Bid + 10
			}
			if a.Bidder == -1 || g.rng.Intn(3) == 0 {
				bid := minBid + 10*g.rng.Intn(3)
				if bid <= p.Cash {
					g.AuctionBid(i, bid)
				} else {
					g.AuctionPass(i)
				}
			} else {
				g.AuctionPass(i)
			}
			if st.Phase != "auction" || st.Auction == nil {
				return
			}
			a = st.Auction
		}
		return
	}

	if st.Phase != "turn" {
		return
	}
	pi := st.Current
	p := &st.Players[pi]
	if p == nil || p.Bankrupt || p.Left {
		return
	}

	// pending buy offer: prioritize completing color groups
	if st.BuyOffer != nil && st.BuyOffer.Player == pi {
		sq := st.BuyOffer.SQ
		want := false
		if st.Board[sq].Kind == KProperty {
			grp := groups[st.Board[sq].Group]
			have := 0
			for _, o := range grp {
				if st.Props[o].Owner == pi {
					have++
				}
			}
			want = have == len(grp)-1 || p.Cash > st.BuyOffer.Price*4
		} else {
			want = p.Cash > 600
		}
		if want && p.Cash >= st.BuyOffer.Price {
			g.Buy(pi)
		} else {
			g.Decline(pi)
		}
		return
	}

	// jail
	if p.InJail && !st.Rolled {
		if p.Cards > 0 {
			g.UseCard(pi)
			return
		}
		if p.Cash > 200 {
			g.PayJail(pi)
			return
		}
	}

	if !st.Rolled {
		// pre-roll: build aggressively on complete groups
		for changed := true; changed; changed = false {
			for sq := range st.Props {
				pr := &st.Props[sq]
				if pr.Owner == pi && st.Board[sq].Kind == KProperty {
					if g.ownsGroup(pi, st.Board[sq].Group) && !pr.Mortgaged && p.Cash > 250 && pr.Houses < 5 {
						if g.Build(pi, sq) == nil {
							changed = true
						}
					}
				}
			}
		}
		g.Roll(pi)
		return
	}

	// post-roll with no pending buy offer: the server auto-advances the
	// turn, so this state should never be observed. Force progress so the
	// hang detection in TestSelfPlay can catch a real bug.
	g.EndTurn(pi)
}

func TestSelfPlay(t *testing.T) {
	terminated := 0
	for game := 0; game < 60; game++ {
		g := NewGame()
		n := 3
		if game%2 == 0 {
			n = 2
		}
		for i := 0; i < n; i++ {
			if _, err := g.Join(string(rune('A' + i))); err != nil {
				t.Fatal(err)
			}
		}
		if err := g.Start(0); err != nil {
			t.Fatal(err)
		}
		prev := ""
		same := 0
		steps := 0
		for g.St.Phase == "turn" || g.St.Phase == "auction" {
			steps++
			if steps > 400000 {
				break // long but legal game; hang detection below is the real check
			}
			sig := testSig(g)
			if sig == prev {
				same++
				if same > 200 {
					t.Fatalf("game %d: state stuck for 200 steps at step %d:\n%s", game, steps, sig)
				}
			} else {
				same = 0
			}
			prev = sig
			aiStep(g)
			checkInvariants(t, g, game)
		}
		if g.St.Phase == "over" && g.St.Winner >= 0 {
			terminated++
		}
	}
	t.Logf("%d/60 games terminated naturally (long games are legal)", terminated)
}

func testSig(g *Game) string {
	b := ""
	for _, p := range g.St.Players {
		b += fmt.Sprintf("%s$c%d@%d j%d bt%v k%d|", p.Name, p.Cash, p.Pos, p.JailTurns, p.Bankrupt, p.Cards)
	}
	a := "none"
	if g.St.Auction != nil {
		a = fmt.Sprintf("sq%d bid%d bidder%d passed%v", g.St.Auction.SQ, g.St.Auction.Bid, g.St.Auction.Bidder, g.St.Auction.Passed)
	}
	return fmt.Sprintf("%s cur=%d dice=%v rolled=%v dd=%v dbl=%d bo=%v auction=%s deals=%d %s",
		g.St.Phase, g.St.Current, g.St.Dice, g.St.Rolled, g.St.LastDouble, g.St.Doubles,
		g.St.BuyOffer, a, len(g.St.Deals), b)
}

func cashes(g *Game) []int {
	out := []int{}
	for _, p := range g.St.Players {
		out = append(out, p.Cash)
	}
	return out
}

func checkInvariants(t *testing.T, g *Game, game int) {
	t.Helper()
	for _, p := range g.St.Players {
		if p.Cash < 0 {
			t.Fatalf("game %d: player %s negative cash %d", game, p.Name, p.Cash)
		}
		if p.Pos < 0 || p.Pos >= 40 {
			t.Fatalf("game %d: bad position %d", game, p.Pos)
		}
	}
	for i, pr := range g.St.Props {
		if pr.Owner >= 0 && !g.St.Players[pr.Owner].Bankrupt && !g.St.Players[pr.Owner].Left {
			if g.St.Board[i].Kind == KProperty && !g.ownsGroup(pr.Owner, g.St.Board[i].Group) && pr.Houses > 0 {
				t.Fatalf("game %d: houses on incomplete group sq %d", game, i)
			}
		}
	}
}

func TestAuctionRules(t *testing.T) {
	g := NewGame()
	for _, n := range []string{"A", "B", "C"} {
		if _, err := g.Join(n); err != nil {
			t.Fatal(err)
		}
	}
	g.Start(0)
	g.mu.Lock()
	g.St.Players[1].Cash = 5000
	g.St.Players[2].Cash = 5000
	g.startAuction(1, 0) // A triggers; square 1 base price
	g.mu.Unlock()

	base := g.St.Board[1].Buy
	if err := g.AuctionBid(1, base-10); err == nil {
		t.Fatal("first bid below base price must fail")
	}
	if err := g.AuctionBid(1, base); err != nil {
		t.Fatalf("first bid at base price must succeed: %v", err)
	}
	if err := g.AuctionBid(0, base+10); err == nil {
		t.Fatal("trigger must not be able to bid")
	}
	if err := g.AuctionBid(2, base+5); err == nil {
		t.Fatal("outbid must be at least $10")
	}
	if err := g.AuctionBid(2, base+15); err == nil {
		t.Fatal("bid must be a multiple of $10")
	}
	if err := g.AuctionBid(2, base+10); err != nil {
		t.Fatalf("valid outbid rejected: %v", err)
	}
	if err := g.AuctionPass(1); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	if g.St.Phase != "turn" || g.St.Props[1].Owner != 2 {
		t.Fatalf("auction should end with C owning square 1: phase=%s owner=%d", g.St.Phase, g.St.Props[1].Owner)
	}
	if g.St.Players[2].Cash != 5000-(base+10) {
		t.Fatalf("C should pay the winning bid: %d", g.St.Players[2].Cash)
	}
	g.mu.Unlock()
}

func TestReplayAfterGameOver(t *testing.T) {
	g := NewGame()
	for _, n := range []string{"A", "B"} {
		if _, err := g.Join(n); err != nil {
			t.Fatal(err)
		}
	}
	// simulate a finished game
	g.mu.Lock()
	g.St.Phase = "over"
	g.St.Winner = 0
	g.St.Players[1].Bankrupt = true
	g.mu.Unlock()

	// new player joining after the game ends must work and reset to lobby
	if _, err := g.Join("C"); err != nil {
		t.Fatalf("join after game over must work: %v", err)
	}
	g.mu.Lock()
	if g.St.Phase != "lobby" || len(g.St.Players) != 3 || g.St.Winner != -1 {
		t.Fatalf("should be in lobby with 3 players, no winner: phase=%s n=%d winner=%d",
			g.St.Phase, len(g.St.Players), g.St.Winner)
	}
	g.mu.Unlock()

	if err := g.Start(0); err != nil {
		t.Fatalf("start after replay reset: %v", err)
	}
	g.mu.Lock()
	for _, p := range g.St.Players {
		if p.Bankrupt || p.Cash != startCash || p.Pos != 0 {
			t.Fatalf("player %d not reset: %+v", p.ID, p)
		}
	}
	g.mu.Unlock()

	// ResetToLobby is only valid from "over"
	if err := g.ResetToLobby(); err == nil {
		t.Fatal("ResetToLobby during an active game must fail")
	}
}
