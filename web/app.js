// ---- Monopoly 2D "Grid War" client ----

const GRID = 9;
const SPAN = GRID - 1;
const PLAYER_COLORS = ["#e05555", "#4a90d9", "#50b86c", "#c98f2f"];
const GROUP_COLORS = ["#8b4513", "#a8d8ea", "#ff9fb2", "#f7941d", "#ff0000", "#b5d33d", "#2e8b57", "#1a1a1a"];
const KIND_EMOJI = { 0: "\u{1F3F0}", 1: "", 2: "\u{1F6E3}\uFE0F", 3: "\u{1F3E6}", 4: "\u26D3\uFE0F", 5: "\u2753", 6: "\u{1F4E6}", 7: "\u{1F4B0}", 8: "\u{1F17F}" };
const BASE_LETTERS = { [8 * GRID + 0]: "A", [8 * GRID + 8]: "B", 0: "C", 8: "D" };
const DIRS = [
  [-1, -1, "\u2196"], [0, -1, "\u2191"], [1, -1, "\u2197"],
  [-1, 0, "\u2190"], [1, 0, "\u2192"],
  [-1, 1, "\u2199"], [0, 1, "\u2193"], [1, 1, "\u2198"],
];

let ws = null;
let myId = -1;
let state = null;
let selActor = -1;   // selected unit id (move phase)
let armDir = null;   // armed direction for two-tap confirmation
let bidDraft = null; // {sq, amount} auction draft

// ---------- helpers ----------
const $ = (id) => document.getElementById(id);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

function send(m) {
  if (ws && ws.readyState === 1) ws.send(JSON.stringify(m));
}
let toastTimer = null;
function toast(msg) {
  const t = $("toast");
  t.textContent = msg;
  t.classList.remove("hidden");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.add("hidden"), 3200);
}

function bounce(coord, steps) {
  let m = (coord + steps) % (2 * SPAN);
  if (m < 0) m += 2 * SPAN;
  if (m > SPAN) m = 2 * SPAN - m;
  return m;
}
function destFor(a, dx, dy) {
  const total = state.dice[0] + state.dice[1];
  return [dx ? bounce(a.x, dx * total) : a.x, dy ? bounce(a.y, dy * total) : a.y];
}
function myPlayers() {
  return state.players.filter((p) => !p.left && p.name);
}
function myPlayer() {
  return state.players[myId];
}
function unitsOf(pid) {
  return state.actors.filter((a) => a.owner === pid && a.alive);
}
function ownedCells(pid) {
  const out = [];
  state.cells.forEach((c, i) => { if (c.owner === pid) out.push(i); });
  return out;
}
function cellLabel(i) {
  const c = state.cells[i];
  return `(${c.x},${c.y})`;
}

// ---------- websocket ----------
function connect() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  ws = new WebSocket(`${proto}://${location.host}/ws`);
  ws.onopen = () => {
    const name = localStorage.getItem("mp2d_name") || "";
    if (name) send({ type: "join", name });
    else showLobbyForm();
  };
  ws.onmessage = (ev) => {
    const m = JSON.parse(ev.data);
    if (m.type === "joined") { myId = m.pid; return; }
    if (m.type === "error") { toast(m.msg); return; }
    if (m.type === "removed") {
      toast("You were removed by the host");
      myId = -1; state = null; selActor = -1;
      showLobbyForm();
      return;
    }
    state = m;
    render();
  };
  ws.onclose = () => setTimeout(connect, 1500);
}

// ---------- lobby ----------
function showLobbyForm() {
  $("game").classList.add("hidden");
  $("lobby").classList.remove("hidden");
  $("lobbyForm").classList.remove("hidden");
  $("lobbyPlayers").classList.add("hidden");
  $("startBtn").classList.add("hidden");
  $("lobbyStatus").textContent = myId === -1 ? "Enter a name to join the lobby." : "Waiting for the lobby…";
}

function showLobby() {
  $("game").classList.add("hidden");
  $("lobby").classList.remove("hidden");
  const alive = myPlayers();
  if (myId === -1) {
    $("lobbyForm").classList.remove("hidden");
    $("lobbyPlayers").classList.add("hidden");
    $("startBtn").classList.add("hidden");
    $("lobbyStatus").textContent = "Waiting for the lobby…";
    return;
  }
  $("lobbyForm").classList.add("hidden");
  $("lobbyPlayers").classList.remove("hidden");
  const lp = $("lobbyPlayers");
  lp.innerHTML = alive.map((p) =>
    `<span class="lobbyPill"><span class="dot" style="background:${PLAYER_COLORS[p.id]}"></span>${esc(p.name)}${p.id === state.host ? " (host)" : ""}${hostRemoveBtn(p)}</span>`
  ).join("");
  bindRemoveBtns(lp);
  const canStart = state.host === myId && alive.length >= 2;
  $("startBtn").classList.toggle("hidden", !canStart);
  $("lobbyStatus").textContent = canStart ? "You are the host. Start when ready." : `Waiting for ${Math.max(0, 2 - alive.length)} more player(s) / the host…`;
}

function hostRemoveBtn(p) {
  if (state.host !== myId || p.id === myId) return "";
  return `<button class="rmBtn" data-pid="${p.id}" title="Remove player">\u2715</button>`;
}
function bindRemoveBtns(container) {
  container.querySelectorAll(".rmBtn").forEach((b) => {
    b.onclick = (e) => {
      e.stopPropagation();
      if (confirm("Remove this player? Their army is eliminated.")) send({ type: "remove", to: +b.dataset.pid });
    };
  });
}

// ---------- render ----------
function render() {
  if (!state) return;
  if (state.phase === "lobby") { showLobby(); return; }
  $("lobby").classList.add("hidden");
  $("game").classList.remove("hidden");
  renderBoard();
  renderPanel();
  renderOver();
}

function renderBoard() {
  const board = $("board");
  board.innerHTML = "";
  let dest = null;
  if (state.phase === "move" && selActor >= 0 && armDir) {
    const a = state.actors.find((u) => u.id === selActor);
    if (a) dest = destFor(a, armDir[0], armDir[1]);
  }
  for (let y = GRID - 1; y >= 0; y--) {
    for (let x = 0; x < GRID; x++) {
      const i = y * GRID + x;
      const c = state.cells[i];
      const d = document.createElement("div");
      d.className = "cell k" + c.kind;
      d.style.gridRow = String(GRID - y);
      d.style.gridColumn = String(x + 1);
      if (c.kind === 1) {
        d.style.background = GROUP_COLORS[c.group] + "44";
        d.innerHTML = `<span class="cprice">$${c.buy}</span>`;
      } else if (c.kind === 0) {
        const bp = state.players.findIndex((p) => p.base === i && !p.left);
        if (bp >= 0) d.style.background = PLAYER_COLORS[bp] + "33";
        d.innerHTML = `<span class="baseL">${BASE_LETTERS[i] || "?"}</span>`;
      } else {
        d.innerHTML = `<span class="cemoji">${KIND_EMOJI[c.kind] || ""}</span>`;
      }
      if (c.owner >= 0) {
        d.classList.add("owned");
        d.style.setProperty("--own", PLAYER_COLORS[c.owner]);
        if (c.kind === 1) {
          if (c.houses > 0) d.innerHTML += `<span class="houses">${"\u{1F3E0}".repeat(c.houses)}</span>`;
          if (c.mortg) d.innerHTML += `<span class="mort">M</span>`;
        }
      }
      // units on this cell
      const here = state.actors.filter((a) => a.alive && a.x === x && a.y === y);
      if (here.length) {
        const uw = document.createElement("div");
        uw.className = "units";
        here.forEach((a) => {
          const u = document.createElement("div");
          u.className = "unit" + (a.jailed ? " jail" : "") + (a.id === selActor ? " picksel" : "");
          u.style.background = PLAYER_COLORS[a.owner];
          u.textContent = a.id;
          if (a.owner === myId && state.phase === "move" && !a.jailed) {
            u.style.cursor = "pointer";
            u.title = "Select this unit";
            u.onclick = () => { selActor = a.id; armDir = null; render(); };
          }
          uw.appendChild(u);
        });
        d.appendChild(uw);
      }
      if (state.phase === "move" && selActor >= 0) {
        const a = state.actors.find((u) => u.id === selActor);
        if (a && a.x === x && a.y === y) d.classList.add("sel");
      }
      if (dest && dest[0] === x && dest[1] === y) d.classList.add("dest");
      board.appendChild(d);
    }
  }
}

function renderPanel() {
  renderTurnInfo();
  renderDice();
  renderActions();
  renderArmy();
  renderEcon();
  renderDeals();
  renderPlayers();
  renderLog();
}

function renderTurnInfo() {
  const el = $("turnInfo");
  const cur = state.players[state.current];
  switch (state.phase) {
    case "roll":
      el.innerHTML = state.current === myId ? `<span class="you">Your turn</span> — fund, build, deal… then roll.` : `Waiting for <b>${esc(cur?.name)}</b>…`;
      break;
    case "move":
      el.innerHTML = `<span class="you">Move</span> — pick one of your units, then a direction.`;
      break;
    case "offer": {
      const o = state.offer;
      const c = state.cells[o.cell];
      el.innerHTML = `Unit #${o.actor} lands on unclaimed land ${cellLabel(o.cell)} — <b>$${c.buy}</b>. Buy it?`;
      break;
    }
    case "auction": {
      const a = state.auction;
      const who = a.bidder >= 0 ? `by <b>${esc(state.players[a.bidder].name)}</b>` : "(no bids yet)";
      el.innerHTML = `Auction for ${cellLabel(a.cell)} — current <b>$${a.bid}</b> ${who}. Min next bid: <b>$${a.bid + 10}</b>.`;
      break;
    }
    case "over":
      el.innerHTML = `<span class="you">${esc(state.players[state.winner]?.name || "Nobody")}</span> wins the war!`;
      break;
  }
}

function renderDice() {
  const el = $("diceRow");
  if ((state.phase === "move" || state.phase === "offer") && state.dice[0]) {
    el.innerHTML = `<span class="die">${state.dice[0]}</span><span class="die">${state.dice[1]}</span><span class="utag" style="opacity:.6">sum ${state.dice[0] + state.dice[1]}</span>`;
  } else el.innerHTML = "";
}

function renderActions() {
  const el = $("actions");
  el.innerHTML = "";
  const mine = state.current === myId;
  if (state.phase === "roll" && mine) {
    el.innerHTML = `
      <button class="btn primary" id="rollBtn">\u{1F3B2} Roll</button>
      <button class="btn" id="recruitBtn">+ Recruit ($300)</button>
      <button class="btn" id="dealBtn">\u{1F91D} Deal</button>`;
    $("rollBtn").onclick = () => send({ type: "roll" });
    $("recruitBtn").onclick = () => send({ type: "reinforce" });
    $("dealBtn").onclick = openDealModal;
  } else if (state.phase === "move" && mine) {
    const a = state.actors.find((u) => u.id === selActor);
    el.innerHTML = `<div id="dpad"></div>` + (a ? `<div style="text-align:center;font-size:12px;opacity:.7">unit #${a.id} at ${cellLabel(state.cells[a.y * GRID + a.x])} — tap a direction to preview, tap again to go</div>` : `<div style="text-align:center;font-size:12px;opacity:.7">select one of your units on the board</div>`);
    buildDpad();
  } else if (state.phase === "offer" && mine) {
    const c = state.cells[state.offer.cell];
    const afford = aCash() >= c.buy;
    el.innerHTML = `
      <button class="btn primary" id="buyBtn" ${afford ? "" : "disabled"}>Buy $${c.buy}</button>
      <button class="btn danger" id="declineBtn">Pass (auction)</button>`;
    $("buyBtn").onclick = () => send({ type: "buy" });
    $("declineBtn").onclick = () => send({ type: "decline" });
  } else if (state.phase === "auction" && mine) {
    const a = state.auction;
    const min = a.bid + 10;
    if (!bidDraft || bidDraft.sq !== a.cell) bidDraft = { sq: a.cell, amount: min };
    if (bidDraft.amount < min) bidDraft.amount = min;
    el.innerHTML = `
      <button class="btn small" id="bm">−$10</button>
      <button class="btn small" id="bp">+$10</button>
      <b id="bval" style="align-self:center">$${bidDraft.amount}</b>
      <button class="btn primary" id="bidBtn">Bid</button>
      <button class="btn danger" id="passBtn">Pass</button>`;
    $("bm").onclick = () => { bidDraft.amount = Math.max(min, bidDraft.amount - 10); renderActions(); };
    $("bp").onclick = () => { bidDraft.amount = Math.min(aCash(), bidDraft.amount + 10); renderActions(); };
    $("bidBtn").onclick = () => { send({ type: "auction_bid", amount: bidDraft.amount }); bidDraft = null; };
    $("passBtn").onclick = () => { send({ type: "auction_pass" }); bidDraft = null; };
  } else if (state.phase !== "over") {
    el.innerHTML = `<span style="opacity:.5;font-size:13px">no actions for you right now</span>`;
  }
}

function aCash() {
  const p = myPlayer();
  return p ? p.cash + unitsOf(myId).reduce((s, a) => s + a.cash, 0) : 0;
}

function buildDpad() {
  const el = $("dpad");
  el.innerHTML = "";
  const layout = [[0, 1, 2], [3, 5, 4], [6, 7, 8]];
  for (const row of layout) {
    for (const idx of row) {
      if (idx === 5) {
        const c = document.createElement("div");
        c.className = "dcenter";
        c.textContent = armDir ? "GO?" : "\u00B7";
        c.title = "Confirm move";
        c.style.cursor = armDir ? "pointer" : "default";
        if (armDir) c.onclick = confirmMove;
        el.appendChild(c);
        continue;
      }
      const [dx, dy, glyph] = DIRS[idx];
      const b = document.createElement("button");
      b.className = "dbtn";
      b.textContent = glyph;
      if (armDir && armDir[0] === dx && armDir[1] === dy) b.style.background = "#1f8f3b";
      b.onclick = () => {
        if (!selActor) { toast("Select one of your units first"); return; }
        if (armDir && armDir[0] === dx && armDir[1] === dy) { confirmMove(); return; }
        armDir = [dx, dy];
        render();
      };
      el.appendChild(b);
    }
  }
}
function confirmMove() {
  const aid = selActor;
  if (!armDir || aid < 0) return;
  const [dx, dy] = armDir;
  armDir = null;
  selActor = -1;
  send({ type: "move", actor: aid, dx, dy });
}

function renderArmy() {
  const el = $("armyPanel");
  el.innerHTML = "";
  if (state.phase === "over") return;
  const my = unitsOf(myId);
  const card = document.createElement("div");
  card.className = "pcard";
  const movable = state.phase === "move";
  let rows = my.map((a) => {
    const canMove = movable && !a.jailed;
    const sel = a.id === selActor;
    return `<div class="arow${sel ? " picksel" : ""}" data-aid="${a.id}" style="${canMove ? "cursor:pointer" : "opacity:.7"}">
      <span class="uchip" style="background:${PLAYER_COLORS[myId]}"></span>
      <span class="uname">U#${a.id}</span>
      <span>$${a.cash}</span>
      <span class="utag">${cellLabel(state.cells[a.y * GRID + a.x])}</span>
      ${a.jailed ? `<span class="utag" style="color:#ff9090">jailed (${a.jailedTurns}/3)</span>` : ""}
      ${state.phase === "roll" && state.current === myId ? `
        <button class="btn small" data-act="fund" data-aid="${a.id}">+$50</button>
        <button class="btn small" data-act="fund100" data-aid="${a.id}">+$100</button>
        ${a.jailed ? `<button class="btn small" data-act="jail" data-aid="${a.id}">Bail $50</button>` : ""}
        <button class="btn small danger" data-act="recall" data-aid="${a.id}">Recall</button>` : ""}
    </div>`;
  }).join("");
  card.innerHTML = `<h3>Your army (${my.length}/${6}) <span class="utag" style="float:right">upkeep $${my.length * 10}/turn</span></h3>${rows || '<span class="utag">no units!</span>'}`;
  card.querySelectorAll(".arow").forEach((row) => {
    row.onclick = (e) => {
      if (e.target.tagName === "BUTTON") return;
      const aid = +row.dataset.aid;
      const a = state.actors.find((u) => u.id === aid);
      if (!movable || !a || a.jailed) return;
      selActor = aid;
      armDir = null;
      render();
    };
  });
  card.querySelectorAll("button[data-act]").forEach((b) => {
    b.onclick = () => {
      const aid = +b.dataset.aid;
      const act = b.dataset.act;
      if (act === "fund") send({ type: "fund", actor: aid, amount: 50 });
      else if (act === "fund100") send({ type: "fund", actor: aid, amount: 100 });
      else if (act === "jail") send({ type: "pay_jail", actor: aid });
      else if (act === "recall") { if (confirm("Recall this unit? Its cash returns to your treasury.")) send({ type: "recall", actor: aid }); }
    };
  });
  el.appendChild(card);
}

function renderEcon() {
  const el = $("econPanel");
  el.innerHTML = "";
  if (state.phase !== "roll" || state.current !== myId) return;
  const mine = ownedCells(myId).filter((i) => state.cells[i].kind === 1);
  if (!mine.length) return;
  const card = document.createElement("div");
  card.className = "pcard";
  card.innerHTML = `<h3>Treasury $${myPlayer().cash} — territory</h3>` + mine.map((i) => {
    const c = state.cells[i];
    const btns = c.mortg
      ? `<button class="btn small" data-cell="${i}" data-act="unm">Unmortgage $${c.buy * 55 / 100}</button>`
      : `<button class="btn small" data-cell="${i}" data-act="m">Mortgage $${c.buy / 2}</button>`;
    const hb = c.houses > 0
      ? `<button class="btn small" data-cell="${i}" data-act="sell">Sell house +$25</button>`
      : `<button class="btn small" data-cell="${i}" data-act="build">Build $50</button>`;
    return `<div class="arow"><span class="uchip" style="background:${GROUP_COLORS[c.group]}"></span>
      <span class="uname">${cellLabel(i)}</span>
      <span class="utag">$${c.buy}${c.houses ? " " + "\u{1F3E0}".repeat(c.houses) : ""}${c.mortg ? " (mortg)" : ""}</span>
      ${hb}${btns}</div>`;
  }).join("");
  card.querySelectorAll("button[data-act]").forEach((b) => {
    b.onclick = () => {
      const cell = +b.dataset.cell, act = b.dataset.act;
      if (act === "m") send({ type: "mortgage", cell });
      else if (act === "unm") send({ type: "unmortgage", cell });
      else if (act === "build") send({ type: "build", cell, sell: false });
      else if (act === "sell") send({ type: "build", cell, sell: true });
    };
  });
  el.appendChild(card);
}

function renderDeals() {
  const el = $("dealsPanel");
  el.innerHTML = "";
  if (!state.deals.length || state.phase === "over") return;
  const card = document.createElement("div");
  card.className = "pcard";
  card.innerHTML = `<h3>Deals</h3>` + state.deals.map((d) => {
    const from = state.players[d.from].name, to = state.players[d.to].name;
    const desc = dealDesc(d);
    if (d.to === myId && state.phase === "roll") {
      return `<div class="dealitem">${esc(from)} offers: <b>${desc}</b>
        <button class="btn small primary" data-d="${d.id}" data-acc="1">Accept</button>
        <button class="btn small danger" data-d="${d.id}" data-acc="0">Decline</button></div>`;
    }
    return `<div class="dealitem">${esc(from)} \u2194 ${esc(to)}: ${desc} <span class="utag">…awaiting</span></div>`;
  }).join("");
  card.querySelectorAll("button[data-d]").forEach((b) => {
    b.onclick = () => send({ type: "respond_deal", deal: +b.dataset.d, accept: b.dataset.acc === "1" });
  });
  el.appendChild(card);
}

function dealDesc(d) {
  const g = [d.give.cash ? `$${d.give.cash}` : "", d.give.props.map(cellLabel).join(",")].filter(Boolean).join(" + ") || "nothing";
  const w = [d.want.cash ? `$${d.want.cash}` : "", d.want.props.map(cellLabel).join(",")].filter(Boolean).join(" + ") || "nothing";
  return `give ${g}, get ${w}`;
}

function renderPlayers() {
  const el = $("playerRow");
  el.innerHTML = myPlayers().map((p) =>
    `<div class="pcard2${p.id === myId ? " mine" : ""}${p.bankrupt ? " dead" : ""}">
      <span class="dot" style="background:${PLAYER_COLORS[p.id]}"></span>
      <span class="pname">${esc(p.name)}${p.id === state.host ? " \u{1F451}" : ""}</span>
      <span class="pmeta">$${p.cash} · ${unitsOf(p.id).length}u</span>
      ${hostRemoveBtn(p)}
    </div>`
  ).join("");
  bindRemoveBtns(el);
}

function renderLog() {
  const el = $("log");
  const lines = state.log.slice(-9).reverse();
  el.innerHTML = `<div class="pcard"><h3>Log</h3>` + lines.map((l) => `<div>${esc(l)}</div>`).join("") + `</div>`;
}

// ---------- deal modal ----------
function openDealModal() {
  const others = myPlayers().filter((p) => p.id !== myId);
  if (!others.length) return;
  const modal = $("modal"), box = $("modalBox");
  box.innerHTML = `<h2>New deal</h2>
    <div class="drow">To: <select id="dTo">${others.map((p) => `<option value="${p.id}">${esc(p.name)}</option>`).join("")}</select></div>
    <div class="drow">I give: <input type="number" id="dGCash" min="0" value="0" placeholder="$">
      <span id="dGProps" style="display:flex;gap:4px;flex-wrap:wrap"></span></div>
    <div class="drow">I get: <input type="number" id="dWCash" min="0" value="0" placeholder="$">
      <span id="dWProps" style="display:flex;gap:4px;flex-wrap:wrap"></span></div>
    <div style="display:flex;gap:8px;justify-content:flex-end">
      <button class="btn" id="dCancel">Cancel</button>
      <button class="btn primary" id="dSend">Propose</button>
    </div>`;
  const refresh = () => {
    const to = +$("dTo").value;
    $("dGProps").innerHTML = ownedCells(myId).map((i) =>
      `<label><input type="checkbox" class="dGp" value="${i}"> ${cellLabel(i)}</label>`).join("") || "<span class='utag'>no properties</span>";
    $("dWProps").innerHTML = ownedCells(to).map((i) =>
      `<label><input type="checkbox" class="dWp" value="${i}"> ${cellLabel(i)}</label>`).join("") || "<span class='utag'>they have none</span>";
  };
  $("dTo").onchange = refresh;
  refresh();
  $("dCancel").onclick = () => modal.classList.add("hidden");
  $("dSend").onclick = () => {
    const give = { cash: +$("dGCash").value || 0, props: [...box.querySelectorAll(".dGp:checked")].map((c) => +c.value) };
    const want = { cash: +$("dWCash").value || 0, props: [...box.querySelectorAll(".dWp:checked")].map((c) => +c.value) };
    modal.classList.add("hidden");
    send({ type: "propose_deal", to: +$("dTo").value, give, want });
  };
  modal.classList.remove("hidden");
}

// ---------- game over ----------
let overEl = null;
function renderOver() {
  if (state.phase !== "over") {
    if (overEl) { overEl.remove(); overEl = null; }
    return;
  }
  if (!overEl) {
    overEl = document.createElement("div");
    overEl.id = "overOverlay";
    document.body.appendChild(overEl);
  }
  const w = state.players[state.winner];
  overEl.innerHTML = `<h1>\u265F ${esc(w ? w.name : "Nobody")} WINS</h1>
    <p style="opacity:.8">The last army standing controls the grid.</p>
    <button class="btn big primary" id="lobbyBtn">Back to lobby</button>`;
  $("lobbyBtn").onclick = () => send({ type: "lobby" });
}

// ---------- lobby events ----------
$("joinBtn").onclick = () => {
  const name = $("nameInput").value.trim();
  if (!name) return;
  localStorage.setItem("mp2d_name", name);
  send({ type: "join", name });
};
$("nameInput").addEventListener("keydown", (e) => { if (e.key === "Enter") $("joinBtn").click(); });
$("startBtn").onclick = () => send({ type: "start" });

connect();
