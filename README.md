# Monopoly 2D — Grid War (experimental branch `2d`)

A non-standard Monopoly experiment: instead of a 40-square circle, the game
happens on a **9×9 grid**, and instead of one pawn per player you command a
small **army of units** with their own funds.

## How it differs from classic Monopoly

- **2D board, billiards movement.** Roll two dice, pick **one unit** and one
  of **8 compass directions**. It slides the dice total and **bounces off the
  walls** (mirror reflection), landing where the reflected ray ends. The
  server validates the endpoint deterministically.
- **Armies.** You start with **3 units** (each holding $200) plus a **$1500
  treasury**. Before rolling you can:
  - **Fund** a unit (treasury → unit, e.g. +$50/+$100)
  - **Recall** a unit (it disappears, cash returns to the treasury)
  - **Recruit** a new unit at your base for **$300** (max 6 units)
  - Build houses / mortgage / propose deals (treasury money)
- **Front lines.** Landing on a cell occupied by **enemy units** costs an
  **occupation fee** per enemy unit ($25 + half the base rent on
  properties). Positioning matters: hold the high-traffic lanes.
- **Capture.** A debt (rent, toll, occupation, tax, card) that the unit *and*
  its treasury cannot cover means the unit is **captured** — the enemy takes
  its remaining cash as spoils. Lose **all** your units and you are
  eliminated (treasury + territory transfer to the captor).
- **Bases.** The 4 corners are fortresses, one per player. Landing on yours:
  **+$100**. Landing on an enemy's: pay a **$50 toll**.
- **2×2 property blocks.** The 8 color groups are 2×2 squares; owning a whole
  block doubles rent and unlocks houses (up to 4). Highways (🛣️) work like
  railroads: $25 → $50 → $100 → $200.
- **Everything else**: bank (+$100), stockade/jail (bail $50 or wait 3
  turns), taxes, chance & chest cards, buy → decline → **auction** (bids in
  $10 steps from treasury), player-to-player **deals** (cash + cells).
- **Army upkeep**: at the start of each turn you pay the bank **$10 per
  surviving unit**. Can't pay? Your poorest unit is abandoned. Big armies
  are expensive — overextension is a real threat.
- **Win**: last army standing.

## Run

```sh
go run .          # http://localhost:8080   (PORT env to change)
go test ./...
```

Open the URL on 2–4 devices, join, host starts.

## Files

- `board.go` — 9×9 map (ASCII layout), cell table, bounce math
- `game.go` — rules engine (turns, movement, occupation, capture, rent,
  buildings, auction, deals, jail, bankruptcy, host/lobby)
- `main.go` — HTTP + WebSocket hub (keep-alive ping/pong, host removal)
- `web/` — responsive client (board grid, army panel, 8-way d-pad with
  bounce preview, deal modal)

## Protocol (client → server)

`join, start, roll, move{actor,dx,dy}, buy, decline, fund{actor,amount},
recall{actor}, reinforce, build{cell,sell}, mortgage{cell}, unmortgage{cell},
pay_jail{actor}, auction_bid{amount}, auction_pass, propose_deal{to,give,want},
respond_deal{deal,accept}, lobby, remove{to}`

Full game state (players, units, cells, dice, auction, deals, log) is
broadcast after every action.
