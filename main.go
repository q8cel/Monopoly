package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Keep-alive: a connection that neither sends messages nor answers pings
// within pongWait is considered dead and is closed, freeing the player's slot.
const (
	pingPeriod = 30 * time.Second
	pongWait   = 90 * time.Second
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Msg struct {
	Type   string  `json:"type"`
	Name   string  `json:"name"`
	To     int     `json:"to"`
	Actor  int     `json:"actor"`
	Dx     int     `json:"dx"`
	Dy     int     `json:"dy"`
	Cell   int     `json:"cell"`
	Sell   bool    `json:"sell"`
	Amount int     `json:"amount"`
	Deal   int     `json:"deal"`
	Accept bool    `json:"accept"`
	Give   *Asset  `json:"give"`
	Want   *Asset  `json:"want"`
}

type outMsg struct {
	Type  string `json:"type"`
	Msg   string `json:"msg,omitempty"`
	Pid   int    `json:"pid"`
	State *State `json:"state,omitempty"`
}

type Hub struct {
	mu    sync.Mutex
	cons  map[int]*websocket.Conn
	game  *Game
	queue chan func()
}

func NewHub(g *Game) *Hub {
	h := &Hub{cons: map[int]*websocket.Conn{}, game: g, queue: make(chan func())}
	go h.run()
	return h
}

func (h *Hub) run() {
	for f := range h.queue {
		f()
	}
}

func (h *Hub) sendRaw(c *websocket.Conn, m outMsg) {
	b, _ := json.Marshal(m)
	c.WriteMessage(websocket.TextMessage, b)
}

func (h *Hub) enqueue(f func()) {
	h.queue <- f
}

func (h *Hub) broadcastState() {
	b := h.game.Snapshot()
	h.enqueue(func() {
		h.mu.Lock()
		for _, c := range h.cons {
			c.WriteMessage(websocket.TextMessage, b)
		}
		h.mu.Unlock()
	})
}

func (h *Hub) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	pingStop := make(chan struct{})
	defer close(pingStop)
	go func() {
		t := time.NewTicker(pingPeriod)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
					conn.Close()
					return
				}
			case <-pingStop:
				return
			}
		}
	}()

	name := r.URL.Query().Get("name")
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
	h.broadcastState()

	defer func() {
		h.mu.Lock()
		if h.cons[pid] == conn {
			delete(h.cons, pid)
		}
		h.mu.Unlock()
		h.game.Disconnect(pid)
		conn.Close()
		h.broadcastState()
	}()

	for {
		var m Msg
		if err := conn.ReadJSON(&m); err != nil {
			return // closed or read timeout (dead connection) -> Disconnect via defer
		}
		conn.SetReadDeadline(time.Now().Add(pongWait)) // any message proves liveness
		h.handle(pid, &m)
	}
}

func (h *Hub) handle(pid int, m *Msg) {
	g := h.game
	var err error
	switch m.Type {
	case "join":
		return
	case "start":
		err = g.Start()
	case "roll":
		err = g.Roll()
	case "move":
		err = g.Move(m.Actor, m.Dx, m.Dy)
	case "buy":
		err = g.Buy()
	case "decline":
		err = g.Decline()
	case "fund":
		err = g.Fund(m.Actor, m.Amount)
	case "recall":
		err = g.Recall(m.Actor)
	case "reinforce":
		err = g.Reinforce()
	case "build":
		err = g.Build(m.Cell, m.Sell)
	case "mortgage":
		err = g.Mortgage(m.Cell)
	case "unmortgage":
		err = g.Unmortgage(m.Cell)
	case "pay_jail":
		err = g.PayJail(m.Actor)
	case "auction_bid":
		err = g.AuctionBid(m.Amount)
	case "auction_pass":
		err = g.AuctionPass()
	case "propose_deal":
		give, want := Asset{}, Asset{}
		if m.Give != nil {
			give = *m.Give
		}
		if m.Want != nil {
			want = *m.Want
		}
		err = g.ProposeDeal(m.To, give, want)
	case "respond_deal":
		err = g.RespondDeal(m.Deal, m.Accept)
	case "lobby":
		err = g.ResetToLobby()
	case "remove":
		if err = g.RemovePlayer(pid, m.To); err == nil {
			h.mu.Lock()
			if c, ok := h.cons[m.To]; ok {
				h.sendRaw(c, outMsg{Type: "removed"})
				c.Close()
			}
			h.mu.Unlock()
		}
	default:
		err = errUnknown(m.Type)
	}
	if err != nil {
		h.enqueue(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if c, ok := h.cons[pid]; ok {
				h.sendRaw(c, outMsg{Type: "error", Msg: err.Error()})
			}
		})
	}
	h.broadcastState()
}

type unknownError string

func (e unknownError) Error() string { return string(e) }

func errUnknown(t string) error { return unknownError("unknown message: " + t) }

func newMux(h *Hub) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", h.handleWS)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		http.ServeFile(w, r, "web/"+p)
	})
	return mux
}

func main() {
	g := NewGame()
	h := NewHub(g)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("monopoly 2d on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, newMux(h)))
}
