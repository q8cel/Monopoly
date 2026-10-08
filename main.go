package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// Msg is a client -> server action message.
type Msg struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	SQ      int    `json:"sq"`
	To      int    `json:"to"`
	Amount  int    `json:"amount"`
	DealID  int    `json:"dealId"`
	Action  string `json:"action"`
	Offer   *Asset `json:"offer"`
	Request *Asset `json:"request"`
}

type outMsg struct {
	Type  string `json:"type"`
	Msg   string `json:"message,omitempty"`
	Pid   int    `json:"pid"`
	State *State `json:"state,omitempty"`
}

type Hub struct {
	mu   sync.Mutex
	cons map[int]*websocket.Conn
	up   websocket.Upgrader
	game *Game
}

func newHub() *Hub {
	h := &Hub{cons: map[int]*websocket.Conn{}, up: websocket.Upgrader{}}
	h.game = NewGame()
	h.game.Broadcast = h.bcast
	return h
}

func (h *Hub) bcast() {
	data, err := json.Marshal(outMsg{Type: "state", State: h.game.St})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.cons {
		c.WriteMessage(websocket.TextMessage, data)
	}
}

func (h *Hub) send(pid int, m outMsg) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.cons[pid]; ok {
		c.WriteMessage(websocket.TextMessage, data)
	}
}

func (h *Hub) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.up.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}
	var join Msg
	if err := conn.ReadJSON(&join); err != nil {
		conn.Close()
		return
	}
	name := strings.TrimSpace(join.Name)
	if name == "" {
		name = "Player"
	}
	pid, err := h.game.Join(name)
	if err != nil {
		h.sendRaw(conn, outMsg{Type: "error", Msg: err.Error()})
		conn.Close()
		return
	}
	h.mu.Lock()
	h.cons[pid] = conn
	h.mu.Unlock()
	h.sendRaw(conn, outMsg{Type: "joined", Pid: pid})
	h.bcast()

	defer func() {
		h.mu.Lock()
		if c, ok := h.cons[pid]; ok && c == conn {
			delete(h.cons, pid)
		}
		h.mu.Unlock()
		h.game.Disconnect(pid)
		h.bcast()
		conn.Close()
	}()

	for {
		var m Msg
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		h.handle(pid, &m)
	}
}

func (h *Hub) sendRaw(c *websocket.Conn, m outMsg) {
	data, _ := json.Marshal(m)
	c.WriteMessage(websocket.TextMessage, data)
}

func (h *Hub) handle(pid int, m *Msg) {
	g := h.game
	var err error
	switch m.Type {
	case "start":
		err = g.Start(pid)
	case "new_game":
		err = g.NewGame(pid)
	case "roll":
		err = g.Roll(pid)
	case "end_turn":
		err = g.EndTurn(pid)
	case "buy":
		err = g.Buy(pid)
	case "decline":
		err = g.Decline(pid)
	case "build":
		err = g.Build(pid, m.SQ)
	case "sell_house":
		err = g.SellHouse(pid, m.SQ)
	case "mortgage":
		err = g.Mortgage(pid, m.SQ)
	case "unmortgage":
		err = g.Unmortgage(pid, m.SQ)
	case "pay_jail":
		err = g.PayJail(pid)
	case "use_card":
		err = g.UseCard(pid)
	case "auction_bid":
		err = g.AuctionBid(pid, m.Amount)
	case "auction_pass":
		err = g.AuctionPass(pid)
	case "propose_deal":
		if m.Offer == nil || m.Request == nil {
			err = errString("missing offer or request")
		} else {
			err = g.ProposeDeal(pid, m.To, *m.Offer, *m.Request)
		}
	case "respond_deal":
		if m.Action == "counter" && (m.Offer == nil || m.Request == nil) {
			err = errString("counter needs offer and request")
		} else {
			err = g.RespondDeal(pid, m.DealID, m.Action, m.Offer, m.Request)
		}
	default:
		err = errBadType(m.Type)
	}
	if err != nil {
		h.send(pid, outMsg{Type: "error", Msg: err.Error()})
		return
	}
	h.bcast()
}

type errString string

func (e errString) Error() string { return string(e) }

func errBadType(t string) error { return errString("unknown action: " + t) }

func newMux() http.Handler {
	h := newHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", h.handleWS)
	mux.Handle("/", http.FileServer(http.Dir("web")))
	return mux
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Println("monopoly server on http://localhost" + addr)
	log.Fatal(http.ListenAndServe(addr, newMux()))
}
