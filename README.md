# Monopoly (web, multiplayer)

Classic Monopoly for 2–4 players in the browser, with one extra rule:
**players can trade with each other** — money, properties (houses & mortgage
status transfer with them) and get-out-of-jail-free cards, with full
accept / counter / decline negotiation.

Everything else follows the classic rules:
- roll & move, doubles (3 in a row → jail), $200 for passing GO
- buy / decline (decline or unable-to-pay starts an **auction** the landing
  player can't bid on)
- rent: color groups (double rent on a full unimproved set), houses/hotels
  with the even-development rule, railroads (25/50/100/200), utilities
  (4×/10× the dice)
- mortgages & unmortgages (10% premium)
- jail: pay $50, use a card, or roll doubles (3 tries, then pay)
- taxes, chance & community chest decks (reshuffle when empty)
- bankruptcy: assets transfer to the creditor (or the bank), last player
  standing wins

Square names are intentionally not assigned yet — squares are shown by
number, color group, price and rent.

## Run

```sh
go run .            # serves http://localhost:8080
PORT=9000 go run .  # custom port
```

Open the URL on any device (desktop or phone), enter a name, join.
The first player (host) starts the game once 2+ players are connected.
All clients stay in sync over WebSockets; the server is authoritative.

## Layout

- `main.go` — HTTP server, WebSocket hub, action dispatch
- `board.go` — the 40-square board definition (kinds, groups, prices, rents)
- `game.go` — all game logic: turns, movement, rent, jail, auction,
  building, mortgages, deals, bankruptcy, cards
- `web/` — single-page client (vanilla JS, no build step):
  - `index.html`, `style.css` — responsive 11×11 board layout (vmin-based,
    works in portrait & landscape on phones)
  - `app.js` — WebSocket client, rendering, action buttons, deal modal
- `*_test.go` — unit tests (deal flow/validation, bankruptcy transfers),
  a WebSocket end-to-end smoke test, and a 60-game AI self-play test that
  checks for hangs and state invariants

## Deal protocol (client → server)

- `propose_deal {to, offer{cash,props[],cards}, request{...}}`
- `respond_deal {dealId, action: accept|decline|counter, offer, request}`

A deal is only applied when the accepting side has enough cash/props/cards;
otherwise it is cancelled with a log entry.
