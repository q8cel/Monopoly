"use strict";

const PCOLORS = ["#e74c3c", "#3498db", "#2ecc71", "#f39c12"];
const KIND_GLYPH = {
  0: "", 1: "", 2: "\u{1F4B0}", 3: "\u{1F682}", 4: "\u26A1", 5: "\u2753", 6: "\u{1F4E6}",
  7: "\u23F8\uFE0F", 8: "\u{1F694}", 9: "\u{1F696}",
};
const GROUP_COLORS = ["#8b4513", "#a8d8ea", "#ff9fb2", "#f7941d", "#ff0000", "#b5d33d", "#2e8b57", "#1a1a1a"];
const KIND_NAMES = { 0: "GO", 1: "Property", 2: "Tax", 3: "Railroad", 4: "Utility", 5: "Chance", 6: "Chest", 7: "Parking", 8: "Jail", 9: "To Jail" };

let ws = null;
let myId = -1;
let state = null;

const $ = (id) => document.getElementById(id);

function connect() {
  ws = new WebSocket(`ws://${location.host}/ws`);
  ws.onopen = () => {
    const name = localStorage.getItem("monopoly_name");
    if (name) send({ type: "join", name });
    else showLobbyForm();
  };
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data);
    if (m.type === "state") { state = m.state; render(); }
    else if (m.type === "joined") { myId = m.pid; }
    else if (m.type === "error") { toast(m.message); }
  };
  ws.onclose = () => { setTimeout(connect, 1500); };
}

function send(obj) { if (ws && ws.readyState === 1) ws.send(JSON.stringify(obj)); }

let toastTimer = null;
function toast(msg) {
  const t = $("toast");
  t.textContent = msg;
  t.classList.remove("hidden");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.add("hidden"), 3500);
}

// ---------- lobby ----------

function showLobbyForm() {
  $("lobby").classList.remove("hidden");
  $("game").classList.add("hidden");
  $("lobbyForm").classList.remove("hidden");
  $("lobbyPlayers").classList.add("hidden");
  $("startBtn").classList.add("hidden");
  $("lobbyStatus").textContent = "Enter your name to join.";
}

function joinGame() {
  const name = $("nameInput").value.trim();
  if (!name) return;
  localStorage.setItem("monopoly_name", name);
  send({ type: "join", name });
}

function showLobby() {
  $("lobby").classList.remove("hidden");
  $("game").classList.add("hidden");
  $("lobbyForm").classList.add("hidden");
  if (!state) return;
  const alive = state.players.filter(p => p.name && !p.left);
  const lp = $("lobbyPlayers");
  lp.classList.remove("hidden");
  lp.innerHTML = alive.map((p) =>
    `<span class="lobbyPill"><span class="dot" style="background:${PCOLORS[p.id % 4]}"></span>${esc(p.name)}${p.id === state.host ? " (host)" : ""}</span>`
  ).join("");
  const canStart = alive.length >= 2;
  $("startBtn").classList.toggle("hidden", !canStart);
  $("startBtn").disabled = state.host !== myId;
  $("startBtn").textContent = state.host === myId ? "Start game" : "Waiting for host to start";
  $("lobbyStatus").textContent = `${alive.length}/4 players connected. Waiting for 2+ players.`;
}

// ---------- render ----------

function esc(s) { return String(s ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])); }

function tilePos(sq) {
  if (sq === 0) return [11, 11];
  if (sq <= 9) return [11, 11 - sq];      // bottom edge, going left
  if (sq === 10) return [11, 1];
  if (sq <= 19) return [21 - sq, 1];      // left edge, going up
  if (sq === 20) return [1, 1];
  if (sq <= 29) return [1, sq - 19];      // top edge, going right
  if (sq === 30) return [1, 11];
  return [sq - 29, 11];                    // right edge, going down
}

function buildBoard() {
  const board = $("board");
  board.querySelectorAll(".tile").forEach((t) => t.remove());
  for (let sq = 0; sq < 40; sq++) {
    const s = state.board[sq];
    const [row, col] = tilePos(sq);
    const t = document.createElement("div");
    t.className = "tile" + ([0, 10, 20, 30, 39].includes(sq) ? " corner" : "");
    t.style.gridRow = row;
    t.style.gridColumn = col;
    t.dataset.sq = sq;

    let strip = "";
    let body;
    const num = `<span class="tnum">${sq}</span>`;
    if (s.kind === 1) {
      strip = `<div class="tstrip" style="background:${GROUP_COLORS[s.group]}"></div>`;
      body = `<div class="tbody"><span class="tname">$${s.buy}</span><span class="tinfo">rent $${s.rent[0]}</span></div>`;
    } else if (s.kind === 3) {
      strip = `<div class="tstrip" style="background:#555"></div>`;
      body = `<div class="tbody"><span class="temoji">\u{1F682}</span><span class="tinfo">$${s.buy}</span></div>`;
    } else if (s.kind === 4) {
      strip = `<div class="tstrip" style="background:#7b5ea7"></div>`;
      body = `<div class="tbody"><span class="temoji">\u26A1</span><span class="tinfo">$${s.buy}</span></div>`;
    } else if (s.kind === 2) {
      body = `<div class="tbody"><span class="temoji">\u{1F4B0}</span><span class="tinfo">tax $${s.tax}</span></div>`;
    } else {
      const glyph = s.kind === 0 ? 'GO' : (KIND_GLYPH[s.kind] || KIND_NAMES[s.kind]);
      body = `<div class="tbody"><span class="temoji${s.kind === 0 ? " goword" : ""}">${glyph}</span><span class="tinfo">${KIND_NAMES[s.kind]}</span></div>`;
    }
    t.innerHTML = num + strip + body;
    board.appendChild(t);
  }
}

function renderTiles() {
  const board = $("board");
  for (const t of board.querySelectorAll(".tile")) {
    const sq = +t.dataset.sq;
    const pr = state.props[sq];
    let extra = t.querySelector(".extra");
    if (extra) extra.remove();
    const e = document.createElement("div");
    e.className = "extra";
    if (pr.owner >= 0 && !state.players[pr.owner].left) {
      e.innerHTML = `<div class="ownerbar" style="background:${PCOLORS[pr.owner % 4]}"></div>`;
      if (pr.mortgaged) e.innerHTML += `<span class="mort">M</span>`;
      if (pr.houses > 0) {
        e.innerHTML += `<span class="houses">${pr.houses >= 5 ? "\u{1F3E8}" : "\u{1F3E0}".repeat(pr.houses)}</span>`;
      }
    }
    const pawns = state.players.filter((p) => p.name && !p.bankrupt && !p.left && p.pos === sq);
    if (pawns.length) {
      e.innerHTML += `<div class="pawns">${pawns.map((p) =>
        `<span class="pawn" style="background:${PCOLORS[p.id % 4]}" title="${esc(p.name)}">${p.id}</span>`).join("")}</div>`;
      t.style.zIndex = 6; // keep pieces above neighboring tiles
    } else {
      t.style.zIndex = "";
    }
    t.appendChild(e);
  }
}

function renderTurnInfo() {
  const cur = state.current >= 0 ? state.players[state.current] : null;
  const d = state.dice;
  let html = "";
  if (state.phase === "auction") {
    html = `<span><b>Auction</b> \u2014 square ${state.auction.sq}</span>`;
    if (state.auction.bidder >= 0) html += `<span>bid: <b>$${state.auction.bid}</b> by ${esc(state.players[state.auction.bidder].name)}</span>`;
    else html += `<span>starting bid: <b>$${state.auction.bid}</b> (bids in $10+ steps)</span>`;
  } else if (cur) {
    html = `<span>Turn: <b style="color:${PCOLORS[cur.id % 4]}">${esc(cur.name)}</b></span>`;
    html += `<span class="dice"><span class="die">${d[0] || "?"}</span><span class="die">${d[1] || "?"}</span></span>`;
    if (cur.inJail) html += `<span>\u{1F694} in jail (${cur.jailTurns}/3)</span>`;
  }
  $("turnInfo").innerHTML = html;
}

function myTurn() { return state.current === myId && state.phase === "turn"; }

function renderActions() {
  const a = $("actions");
  a.innerHTML = "";
  if (state.phase === "over") return;
  const row = document.createElement("div");
  row.className = "btnrow";
  const btn = (label, cls, fn, disabled) => {
    const b = document.createElement("button");
    b.className = "btn " + (cls || "");
    b.textContent = label;
    b.disabled = !!disabled;
    b.onclick = fn;
    row.appendChild(b);
    return b;
  };

  const me = state.players[myId];
  const bo = state.buyOffer;
  if (me.bankrupt || me.left) {
    a.appendChild(row);
    return;
  }
  if (myTurn()) {
    if (bo && bo.player === myId) {
      btn(`Buy #${bo.sq} ($${bo.price})`, "green", () => send({ type: "buy" }), me.cash < bo.price);
      btn("Decline \u2192 auction", "danger", () => send({ type: "decline" }));
    } else if (!state.rolled) {
      btn("\u{1F3B2} Roll dice", "primary", () => send({ type: "roll" }));
      btn("\u{1F91D} Deal", "", () => openDealModal(null));
      btn("Build / manage", "", () => openManageModal());
      if (me.inJail) {
        btn("Pay $50 (jail)", "", () => send({ type: "pay_jail" }), me.cash < 50);
        btn("Use card (jail)", "", () => send({ type: "use_card" }), me.cards === 0);
      }
    } else {
      const w = document.createElement("span");
      w.style.fontSize = "clamp(8px,1.6vmin,13px)";
      w.style.opacity = ".7";
      w.textContent = "\u23F3 waiting for next turn\u2026";
      row.appendChild(w);
    }
  }
  a.appendChild(row);
}

let bidDraft = null; // {sq, amount} - survives re-renders

function renderAuction() {
  const a = $("auctionPanel");
  a.innerHTML = "";
  if (state.phase !== "auction" || !state.auction) { bidDraft = null; return; }
  const au = state.auction;
  const me = state.players[myId];
  if (me.bankrupt || me.left || me.id === au.trigger) return;
  const passed = au.passed.includes(myId);
  const minBid = au.bidder >= 0 ? au.bid + 10 : au.bid;
  if (!bidDraft || bidDraft.sq !== au.sq) bidDraft = { sq: au.sq, amount: minBid };
  bidDraft.amount = Math.max(bidDraft.amount, minBid);

  const row = document.createElement("div");
  row.className = "btnrow";
  const btn = (label, cls, fn, disabled) => {
    const b = document.createElement("button");
    b.className = "btn " + (cls || "");
    b.textContent = label;
    b.disabled = !!disabled;
    b.onclick = fn;
    row.appendChild(b);
    return b;
  };
  btn("\u221210", "", () => { bidDraft.amount -= 10; renderAuction(); }, passed || bidDraft.amount <= minBid);
  const val = document.createElement("span");
  val.textContent = "$" + bidDraft.amount;
  val.style.cssText = "font-weight:700;min-width:4.5em;text-align:center;align-self:center;";
  row.appendChild(val);
  btn("+10", "", () => { bidDraft.amount += 10; renderAuction(); }, passed);
  btn("Bid", "primary", () => send({ type: "auction_bid", amount: bidDraft.amount }), passed);
  btn("Pass", "", () => send({ type: "auction_pass" }), passed);
  if (passed) {
    const s = document.createElement("span");
    s.textContent = "you passed";
    s.style.fontSize = "12px";
    row.appendChild(s);
  }
  a.appendChild(row);
}

function assetLabel(asset) {
  return [
    asset.cash ? `$${asset.cash}` : "",
    asset.props && asset.props.length ? asset.props.map((p) => `#${p}`).join(",") : "",
    asset.cards ? `${asset.cards}x card` : "",
  ].filter(Boolean).join(" + ") || "nothing";
}

function renderDeals() {
  const d = $("dealsPanel");
  d.innerHTML = "";
  const mine = (state.deals || []).filter((x) => x.from === myId || x.to === myId);
  for (const deal of mine) {
    const box = document.createElement("div");
    box.className = "btnrow";
    box.style.marginBottom = "4px";
    const giver = state.players[deal.from];
    const taker = state.players[deal.to];
    const info = document.createElement("span");
    info.style.fontSize = "clamp(7px,1.5vmin,12px)";
    info.innerHTML = `<b>#${deal.id}</b> ${esc(giver.name)} gives ${esc(assetLabel(deal.offer))} &harr; ${esc(taker.name)} wants ${esc(assetLabel(deal.request))}`;
    box.appendChild(info);
    const b = (t, cls, fn) => {
      const x = document.createElement("button");
      x.className = "btn " + (cls || "");
      x.textContent = t;
      x.onclick = fn;
      box.appendChild(x);
    };
    b("Accept", "green", () => send({ type: "respond_deal", dealId: deal.id, action: "accept" }));
    b("Counter", "", () => openDealModal(deal));
    b("Decline", "danger", () => send({ type: "respond_deal", dealId: deal.id, action: "decline" }));
    if (deal.from === myId) {
      const s = document.createElement("span");
      s.textContent = "waiting for " + state.players[deal.to].name;
      s.style.fontSize = "11px";
      s.style.opacity = ".7";
      box.appendChild(s);
    }
    d.appendChild(box);
  }
}

function renderPlayers() {
  const r = $("playerRow");
  r.innerHTML = state.players.filter((p) => p.name && !p.left).map((p) => {
    const cls = p.bankrupt ? "out" : (p.id === state.current && state.phase === "turn" ? "turn" : "");
    return `<span class="pcard ${cls}"><span class="dot" style="background:${PCOLORS[p.id % 4]}"></span>${esc(p.name)} <b>$${p.cash}</b>${p.inJail ? " \u{1F694}" : ""}${p.cards ? ` \u{1F39F}x${p.cards}` : ""}</span>`;
  }).join("");
}

function renderLog() {
  const l = $("log");
  l.innerHTML = (state.log || []).map((x) => `<div>${esc(x)}</div>`).join("");
  l.scrollTop = l.scrollHeight;
}

function showGameOver() {
  if (!$("overOverlay")) {
    const ov = document.createElement("div");
    ov.id = "overOverlay";
    ov.style.cssText = "position:fixed;inset:0;background:rgba(0,0,0,.7);z-index:40;display:flex;align-items:center;justify-content:center;";
    ov.innerHTML = `<div id="overBox"><div class="winner"></div><button class="btn big primary" id="lobbyBtn">Lobby</button></div>`;
    document.body.appendChild(ov);
    $("lobbyBtn").onclick = () => send({ type: "lobby" });
  }
  $("overBox").querySelector(".winner").textContent =
    state.winner >= 0 ? `${state.players[state.winner].name} wins!` : "Game over";
}
function hideGameOver() {
  const ov = $("overOverlay");
  if (ov) ov.remove();
}

function render() {
  if (!state) return;
  if (state.phase === "lobby") {
    showLobby();
    return;
  }
  $("lobby").classList.add("hidden");
  $("game").classList.remove("hidden");
  buildBoard();
  renderTiles();
  renderTurnInfo();
  renderActions();
  renderAuction();
  renderDeals();
  renderPlayers();
  renderLog();
  if (state.phase === "over") showGameOver();
}

const _render = render;
render = function () {
  if (state && state.phase !== "over") hideGameOver();
  _render();
};

// ---------- modals ----------

function openModal(html) {
  $("modalBox").innerHTML = html;
  $("modal").classList.remove("hidden");
}
function closeModal() { $("modal").classList.add("hidden"); }

function openManageModal() {
  const me = state.players[myId];
  const props = state.props
    .map((pr, sq) => ({ ...pr, sq }))
    .filter((x) => x.owner === myId);
  const rows = props.map((x) => {
    const s = state.board[x.sq];
    const isProp = s.kind === 1;
    const groupOk = isProp && groupMembers(x.sq).every((o) => state.props[o].owner === myId);
    const btns = [];
    if (isProp && !x.mortgaged && x.houses < 5) btns.push(`<button class="btn green" data-act="build" data-sq="${x.sq}" ${groupOk ? "" : "disabled"}>Build $100</button>`);
    if (isProp && x.houses > 0) btns.push(`<button class="btn" data-act="sell_house" data-sq="${x.sq}">Sell $50</button>`);
    if (!x.mortgaged) btns.push(`<button class="btn" data-act="mortgage" data-sq="${x.sq}">Mortgage +$${mort(x.sq)}</button>`);
    else btns.push(`<button class="btn" data-act="unmortgage" data-sq="${x.sq}">Unmortgage $${Math.floor(mort(x.sq) * 1.1)}</button>`);
    const glyph = isProp ? "\u{1F3E0}" : s.kind === 3 ? "\u{1F682}" : "\u26A1";
    const label = `${glyph} #${x.sq} ($${s.buy})` +
      (x.houses ? ` &middot; ${x.houses >= 5 ? "hotel" : x.houses + " house(s)"}` : "") +
      (x.mortgaged ? " &middot; MORTGAGED" : "");
    return `<div class="mrow"><span style="flex:1;min-width:120px">${label}</span>${btns.join("")}</div>`;
  }).join("");
  openModal(`
    <h2>Your properties</h2>
    <div class="small" style="color:#999">Cash: $${me.cash}</div>
    ${rows || '<div class="small" style="color:#999">You own nothing yet.</div>'}
    <div class="mrow" style="justify-content:flex-end"><button class="btn" id="mClose">Close</button></div>
  `);
  $("mClose").onclick = closeModal;
  $("modalBox").querySelectorAll("button[data-act]").forEach((b) => {
    b.onclick = () => {
      send({ type: b.dataset.act, sq: +b.dataset.sq });
      closeModal();
    };
  });
}

function mort(sq) {
  const v = Math.floor(state.board[sq].buy / 2);
  return v - v % 5;
}

function groupMembers(sq) {
  const g = state.board[sq].group;
  return state.props.map((p, i) => i).filter((i) => state.board[i].kind === 1 && state.board[i].group === g);
}

function openDealModal(deal) {
  const others = state.players.filter((p) => !p.left && !p.bankrupt && p.id !== myId);
  if (!others.length) { toast("No one to deal with"); return; }
  const otherId = deal ? (deal.from === myId ? deal.to : deal.from) : others[0].id;
  const me = state.players[myId];
  const other = state.players[otherId];

  const giveProps = deal ? (deal.from === myId ? deal.offer.props : deal.request.props) : [];
  const getProps = deal ? (deal.from === myId ? deal.request.props : deal.offer.props) : [];
  const giveCash = deal ? (deal.from === myId ? deal.offer.cash : deal.request.cash) : 0;
  const getCash = deal ? (deal.from === myId ? deal.request.cash : deal.offer.cash) : 0;
  const giveCards = deal ? (deal.from === myId ? deal.offer.cards : deal.request.cards) : 0;
  const getCards = deal ? (deal.from === myId ? deal.request.cards : deal.offer.cards) : 0;

  const propCheckboxes = (owner, preselected, prefix) => {
    const owned = state.props
      .map((pr, sq) => ({ ...pr, sq }))
      .filter((x) => x.owner === owner && [1, 3, 4].includes(state.board[x.sq].kind));
    if (!owned.length) return '<span class="small">(no properties)</span>';
    return owned.map((x) =>
      `<label class="mprop"><input type="checkbox" class="${prefix}" data-sq="${x.sq}" ${preselected.includes(x.sq) ? "checked" : ""}> #${x.sq} ($${state.board[x.sq].buy})</label>`
    ).join("");
  };

  openModal(`
    <h2>${deal ? "Counter deal #" + deal.id : "Propose a deal"}</h2>
    <div class="mrow">With: <select id="dOther">${others.map((p) => `<option value="${p.id}" ${p.id === otherId ? "selected" : ""}>${esc(p.name)}</option>`).join("")}</select></div>
    <h3>You give</h3>
    <div class="mrow">Cash: <input type="number" id="dGiveCash" min="0" value="${giveCash || 0}"></div>
    <div class="mrow" style="align-items:flex-start">${propCheckboxes(myId, giveProps, "dGiveProp")}</div>
    <label class="mrow"><input type="checkbox" id="dGiveCard" ${giveCards > 0 ? "checked" : ""}> jail-free card (you have ${me.cards})</label>
    <h3>You get</h3>
    <div class="mrow">Cash: <input type="number" id="dGetCash" min="0" value="${getCash || 0}"></div>
    <div class="mrow" style="align-items:flex-start">${propCheckboxes(otherId, getProps, "dGetProp")}</div>
    <label class="mrow"><input type="checkbox" id="dGetCard" ${getCards > 0 ? "checked" : ""}> jail-free card (they have ${other.cards})</label>
    <div class="mrow" style="justify-content:flex-end">
      <button class="btn danger" id="dCancel">Cancel</button>
      <button class="btn primary" id="dSend">${deal ? "Send counter" : "Send deal"}</button>
    </div>
  `);
  $("dCancel").onclick = closeModal;
  $("dSend").onclick = () => {
    const to = +$("dOther").value;
    const offer = {
      cash: +($("dGiveCash").value || 0),
      props: [...document.querySelectorAll(".dGiveProp:checked")].map((c) => +c.dataset.sq),
      cards: $("dGiveCard").checked ? 1 : 0,
    };
    const request = {
      cash: +($("dGetCash").value || 0),
      props: [...document.querySelectorAll(".dGetProp:checked")].map((c) => +c.dataset.sq),
      cards: $("dGetCard").checked ? 1 : 0,
    };
    if (deal) {
      send({ type: "respond_deal", dealId: deal.id, action: "counter", offer, request });
    } else {
      send({ type: "propose_deal", to, offer, request });
    }
    closeModal();
  };
}

// ---------- board tile click ----------

$("board").addEventListener("click", (e) => {
  const t = e.target.closest(".tile");
  if (!t || !state || state.phase === "over") return;
  const sq = +t.dataset.sq;
  const pr = state.props[sq];
  const s = state.board[sq];
  const me = state.players[myId];
  if (!me || me.bankrupt || me.left) return;
  if (pr.owner === myId) {
    openManageModal();
  } else if (pr.owner >= 0 && !state.players[pr.owner].left) {
    const owner = state.players[pr.owner];
    let rentInfo;
    if (s.kind === 1) rentInfo = "rent " + s.rent.join(" / ");
    else if (s.kind === 3) rentInfo = "rent 25/50/100/200 by # owned";
    else rentInfo = "rent 4x or 10x dice";
    toast(`#${sq} \u2014 owned by ${owner.name}. ${rentInfo}${pr.mortgaged ? " (mortgaged, no rent)" : ""}`);
  }
});

$("modal").addEventListener("click", (e) => { if (e.target === $("modal")) closeModal(); });

// ---------- boot ----------

$("joinBtn").onclick = joinGame;
$("nameInput").addEventListener("keydown", (e) => { if (e.keyCode === 13) joinGame(); });
$("startBtn").onclick = () => send({ type: "start" });
connect();
