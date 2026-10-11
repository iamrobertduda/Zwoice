import { Voice, distanceGain } from "./voice.js";

const $ = (id) => document.getElementById(id);
const defaults = {
  name: "",
  sauce: "http://127.0.0.1:1080",
  server: "",
  token: "",
  inputDevice: "",
  outputDevice: "",
  noiseSuppression: true,
  pushToTalk: false,
  relayOnly: false,
  volume: 80,
  remember: false,
};
let stored = {};
try {
  stored = JSON.parse(localStorage.getItem("zwoice.settings") || "{}");
} catch {}
let settings = { ...defaults, ...stored };
fetch("/config.json")
  .then((r) => r.json())
  .then((config) => {
    if (!settings.server && typeof config.server === "string")
      settings.server = config.server;
  })
  .catch(() => {});
let socket,
  wanted = false,
  demo = false,
  ready = false,
  sauceConnected = false,
  retryTimer,
  attempt = 0,
  lastMessage = 0,
  peers = [],
  requestGeneration = 0;
const rows = new Map(),
  dots = new Map(),
  peerStates = new Map();
const voice = new Voice(send, updateAudioStatus, (message, fatal = false) => {
  notice(message, true);
  if (fatal) stop(false);
});
try {
  const mutes = JSON.parse(localStorage.getItem("zwoice.mutes") || "[]");
  if (Array.isArray(mutes))
    voice.mutedPeers = new Set(
      mutes.filter((x) => typeof x === "string").slice(-256),
    );
} catch {}
voice.volume = Number(settings.volume) / 100;
$("volume").value = settings.volume;
$("volumeValue").value = `${settings.volume} %`;
const worlds = {
  1: "Watopia",
  2: "Richmond",
  3: "London",
  4: "New York",
  5: "Innsbruck",
  6: "Bologna",
  7: "Yorkshire",
  8: "Crit City",
  9: "Makuri Islands",
  10: "France",
  11: "Paris",
  12: "Gravel Mountain",
  13: "Scotland",
};

function notice(text, error = false) {
  const box = $("notice");
  box.hidden = !text;
  box.textContent = text;
  box.classList.toggle("error", error);
}
function send(message) {
  if (socket?.readyState === WebSocket.OPEN && ready)
    socket.send(JSON.stringify(message));
}
function saveSettings() {
  try {
    localStorage.setItem(
      "zwoice.settings",
      JSON.stringify({
        ...settings,
        token: settings.remember ? settings.token : "",
      }),
    );
  } catch {
    notice("Einstellungen konnten in diesem Browser nicht gespeichert werden.");
  }
}
function badge(text, active = false) {
  $("sessionBadge").replaceChildren();
  const dot = document.createElement("span");
  dot.className = "status-dot";
  $("sessionBadge").append(dot, document.createTextNode(text));
  $("sessionBadge").classList.toggle("active", active);
}
function connection(kind, active, text) {
  $(`${kind}Indicator`).classList.toggle("active", active);
  $(`${kind}Detail`).textContent = text;
}

function showSettings() {
  $("displayName").value = settings.name;
  $("sauceURL").value = settings.sauce;
  $("serverURL").value = settings.server;
  $("inviteToken").value = settings.token;
  $("rememberInvite").checked = settings.remember;
  $("noiseSuppression").checked = settings.noiseSuppression;
  $("pushToTalk").checked = settings.pushToTalk;
  $("relayOnly").checked = settings.relayOnly;
  $("settingsDialog").showModal();
  refreshDevices();
}
async function refreshDevices() {
  if (!navigator.mediaDevices?.enumerateDevices) return;
  try {
    const devices = await navigator.mediaDevices.enumerateDevices();
    for (const [id, kind, key] of [
      ["inputDevice", "audioinput", "inputDevice"],
      ["outputDevice", "audiooutput", "outputDevice"],
    ]) {
      const select = $(id);
      select.replaceChildren(new Option("Systemstandard", ""));
      devices
        .filter(
          (d) => d.kind === kind && d.deviceId && d.deviceId !== "default",
        )
        .forEach((d, i) =>
          select.add(new Option(d.label || `Gerät ${i + 1}`, d.deviceId)),
        );
      select.value = settings[key];
      if (select.selectedIndex < 0) select.value = "";
    }
    const outputSupported = "setSinkId" in HTMLMediaElement.prototype;
    $("outputDevice").disabled = !outputSupported;
    $("outputHelp").textContent = outputSupported
      ? "Die Geräteauswahl wird beim nächsten Fahrtstart übernommen."
      : "Dieser Browser nutzt die Systemausgabe. Ändere das Ausgabegerät in den Systemeinstellungen.";
  } catch {
    notice("Audiogeräte konnten nicht gelesen werden.", true);
  }
}
$("settingsForm").addEventListener("submit", (event) => {
  event.preventDefault();
  const wasActive = wanted;
  if (wasActive) stop(false);
  settings = {
    ...settings,
    name: $("displayName").value.trim(),
    sauce: $("sauceURL").value.trim(),
    server: $("serverURL").value.trim(),
    token: $("inviteToken").value.trim(),
    remember: $("rememberInvite").checked,
    inputDevice: $("inputDevice").value,
    outputDevice: $("outputDevice").value,
    noiseSuppression: $("noiseSuppression").checked,
    pushToTalk: $("pushToTalk").checked,
    relayOnly: $("relayOnly").checked,
  };
  saveSettings();
  $("settingsDialog").close();
  notice(
    wasActive
      ? "Einstellungen gespeichert. Starte die Fahrt neu, um sie zu verwenden."
      : "Alles gespeichert. Du kannst deine Fahrt starten.",
  );
});
$("forgetButton").onclick = () => {
  stop(false);
  try {
    localStorage.removeItem("zwoice.settings");
    localStorage.removeItem("zwoice.mutes");
  } catch {}
  settings = { ...defaults };
  voice.mutedPeers.clear();
  voice.volume = 0.8;
  $("volume").value = 80;
  $("volumeValue").value = "80 %";
  $("settingsDialog").close();
  notice("Gespeicherte Einstellungen und Einladung gelöscht.");
};
$("quitButton").onclick = async () => {
  stop();
  try {
    const response = await fetch("/api/quit", { method: "POST" });
    if (!response.ok) throw new Error();
    notice("Zwoice wurde beendet. Du kannst dieses Fenster schließen.");
    $("connectButton").disabled = true;
  } catch {
    notice(
      "Bitte Zwoice über das Terminal oder den Prozessmanager beenden.",
      true,
    );
  }
};
for (const id of ["settingsNav", "audioSettingsButton"])
  $(id).onclick = showSettings;
$("helpNav").onclick = () => $("helpDialog").showModal();
$("rideNav").onclick = () => window.scrollTo({ top: 0, behavior: "smooth" });
document
  .querySelectorAll("[data-close]")
  .forEach((b) => (b.onclick = () => $(b.dataset.close).close()));

$("connectButton").onclick = () => (wanted ? stop() : start());
async function start() {
  if (!settings.name || !settings.server || !settings.token) {
    showSettings();
    return;
  }
  if (
    !navigator.mediaDevices?.getUserMedia ||
    !window.RTCPeerConnection ||
    !window.AudioContext
  ) {
    notice(
      "Dieser Browser unterstützt den Sprachchat nicht. Bitte einen aktuellen Desktop-Browser verwenden.",
      true,
    );
    return;
  }
  if (demo) exitDemo();
  wanted = true;
  attempt = 0;
  const generation = ++requestGeneration;
  $("connectButton").textContent = "Abbrechen";
  badge("Mikrofon wird vorbereitet");
  notice("Bitte erlaube den Zugriff auf dein Mikrofon.");
  try {
    const opened = await voice.open(settings);
    if (!opened || generation !== requestGeneration || !wanted) return;
    notice("Verbinde mit deiner Fahrt …");
    refreshDevices();
    connect();
    renderControls();
  } catch (error) {
    if (generation !== requestGeneration) return;
    stop(false);
    notice(
      error.name === "NotAllowedError"
        ? "Mikrofonzugriff nicht erlaubt. Bitte in den Browser-Einstellungen freigeben und erneut starten."
        : error.name === "NotFoundError"
          ? "Kein Mikrofon gefunden. Bitte ein Audiogerät anschließen."
          : error.name === "OverconstrainedError"
            ? "Das gewählte Mikrofon ist nicht mehr verfügbar. Bitte Systemstandard auswählen."
            : `Audio konnte nicht gestartet werden: ${error.message}`,
      true,
    );
  }
}
function connect() {
  if (!wanted) return;
  ready = false;
  sauceConnected = false;
  voice.setAllowed(false);
  lastMessage = Date.now();
  const ws = new WebSocket(`ws://${location.host}/api/session`);
  socket = ws;
  badge(
    attempt ? "Verbindung wird wiederhergestellt" : "Verbinde mit deiner Fahrt",
  );
  connection("server", false, "Verbindung wird aufgebaut");
  ws.onopen = () => {
    if (socket === ws)
      ws.send(
        JSON.stringify({
          server: settings.server,
          sauce: settings.sauce,
          token: settings.token,
          name: settings.name,
        }),
      );
  };
  ws.onmessage = ({ data }) => {
    if (socket !== ws) return;
    lastMessage = Date.now();
    try {
      const m = JSON.parse(data);
      if (m.type === "welcome") {
        voice.configure(m.id, m.iceServers);
        ready = true;
        attempt = 0;
        send({ type: "voice", active: true });
        connection("server", true, "Verbunden · Sprache verschlüsselt");
        $("connectButton").textContent = "Fahrt beenden";
        notice("Warte auf deine eigene Position aus Sauce …");
        badge("Warte auf Sauce");
      } else if (m.type === "sauce") {
        const changed = sauceConnected !== m.connected;
        sauceConnected = m.connected;
        voice.setAllowed(sauceConnected);
        connection(
          "sauce",
          sauceConnected,
          sauceConnected ? `Fahrer ${m.athleteId} · Live-Daten` : m.message,
        );
        $("liveDot").classList.toggle("active", sauceConnected);
        if (sauceConnected) {
          $("worldLabel").textContent =
            worlds[m.course] || `Zwift-Welt ${m.course}`;
          badge("Du bist in Hörweite", true);
          $("selfLabel").textContent = "Du bist dabei";
          if (changed) notice("");
        } else {
          badge("Sprache pausiert");
          renderPeers([]);
          notice(m.message);
          $("selfLabel").textContent = "Pausiert";
        }
        renderControls();
      } else if (m.type === "peers") {
        if (sauceConnected) {
          peers = m.peers;
          voice.sync(peers);
          renderPeers(peers);
        }
      } else if (m.type === "signal") {
        voice.signal(m.from, m.payload);
      } else if (m.type === "ice") {
        voice.configure(voice.id, m.iceServers);
      } else if (m.type === "error") {
        notice(m.message, true);
        if (m.fatal !== false) stop(false);
      }
    } catch (error) {
      notice(
        error.message || "Verbindung konnte nicht verarbeitet werden.",
        true,
      );
      stop(false);
    }
  };
  ws.onclose = () => {
    if (socket !== ws) return;
    ready = false;
    sauceConnected = false;
    voice.setAllowed(false);
    renderPeers([]);
    renderControls();
    connection("server", false, "Verbindung unterbrochen");
    connection("sauce", false, "Sprache sicher pausiert");
    $("liveDot").classList.remove("active");
    if (wanted) {
      badge("Verbindung wird wiederhergestellt");
      const delay =
        Math.min(15000, 700 * 2 ** Math.min(attempt++, 5)) +
        Math.random() * 400;
      notice(
        "Verbindung unterbrochen. Zwoice verbindet sich automatisch erneut. Falls ein zweiter Tab geöffnet ist, schließe ihn.",
      );
      retryTimer = setTimeout(connect, delay);
    }
  };
  ws.onerror = () => {
    /* onclose owns retry and cleanup */
  };
}
function stop(clearNotice = true) {
  wanted = false;
  ready = false;
  sauceConnected = false;
  ++requestGeneration;
  clearTimeout(retryTimer);
  const old = socket;
  socket = null;
  old?.close();
  voice.close();
  peers = [];
  peerStates.clear();
  renderPeers([]);
  renderControls();
  $("connectButton").textContent = "Fahrt starten";
  $("liveDot").classList.remove("active");
  $("worldLabel").textContent = "Warte auf deine Fahrt";
  $("selfLabel").textContent = "Startklar";
  $("latency").textContent = "—";
  connection("sauce", false, "Wartet auf Verbindung");
  connection("server", false, "Noch nicht verbunden");
  badge("Bereit, wenn du es bist");
  if (clearNotice) notice("");
}
setInterval(() => {
  if (
    wanted &&
    socket?.readyState === WebSocket.OPEN &&
    Date.now() - lastMessage > 7000
  ) {
    voice.setAllowed(false);
    socket.close();
  }
}, 1000);

function renderControls() {
  $("micButton").disabled = !voice.stream && !demo;
  $("micButton").setAttribute("aria-pressed", String(voice.muted));
  $("micLabel").textContent = demo
    ? voice.muted
      ? "Mikrofon stumm · Demo"
      : "Mikrofon bereit · Demo"
    : !voice.stream
      ? "Mikrofon inaktiv"
      : !sauceConnected
        ? "Mikrofon pausiert"
        : voice.muted
          ? "Mikrofon stumm"
          : voice.deafened
            ? "Audio pausiert"
            : voice.pushToTalk && !voice.pressed
              ? "Zum Sprechen Leertaste halten"
              : "Mikrofon an";
  $("micHint").textContent = demo
    ? "Kein echter Mikrofonzugriff"
    : !voice.stream
      ? "Wird beim Fahrtstart freigegeben"
      : voice.pushToTalk
        ? "Leertaste · nur bei aktivem Fenster"
        : "Klicken zum Stummschalten";
  $("audioBadge").textContent = demo
    ? "Vorschau"
    : voice.stream
      ? sauceConnected
        ? "Bereit"
        : "Pausiert"
      : "Inaktiv";
  $("deafenButton").textContent = voice.deafened
    ? "Ton fortsetzen"
    : "Ton pausieren";
  $("deafenButton").setAttribute("aria-pressed", String(voice.deafened));
  if (!voice.stream) $("micMeter").style.width = "0%";
}
$("micButton").onclick = () => {
  voice.muted = !voice.muted;
  voice.updateControls();
  renderControls();
};
$("deafenButton").onclick = () => {
  voice.deafened = !voice.deafened;
  voice.updateControls();
  renderControls();
};
$("volume").oninput = () => {
  settings.volume = Number($("volume").value);
  voice.volume = settings.volume / 100;
  $("volumeValue").value = `${settings.volume} %`;
  voice.updateControls();
  saveSettings();
};
window.addEventListener("keydown", (e) => {
  if (
    e.code === "Space" &&
    wanted &&
    voice.pushToTalk &&
    !/INPUT|TEXTAREA|SELECT/.test(e.target.tagName) &&
    !document.querySelector("dialog[open]")
  ) {
    e.preventDefault();
    voice.pressed = true;
    voice.updateControls();
    renderControls();
  }
});
function releaseTalk() {
  voice.pressed = false;
  voice.updateControls();
  renderControls();
}
window.addEventListener("keyup", (e) => {
  if (e.code === "Space") releaseTalk();
});
window.addEventListener("blur", releaseTalk);
document.addEventListener("visibilitychange", () => {
  if (document.hidden) releaseTalk();
});
window.addEventListener("pagehide", () => stop(false));
navigator.mediaDevices?.addEventListener("devicechange", () =>
  refreshDevices(),
);

function make(tag, className, text) {
  const el = document.createElement(tag);
  el.className = className;
  if (text !== undefined) el.textContent = text;
  return el;
}
function initials(name) {
  return name
    .trim()
    .split(/\s+/)
    .slice(0, 2)
    .map((n) => n[0])
    .join("")
    .toUpperCase();
}
const empty = $("peopleList").firstElementChild;
function renderPeers(list) {
  const ids = new Set(list.map((p) => p.id));
  for (const [id, row] of rows)
    if (!ids.has(id)) {
      row.remove();
      rows.delete(id);
      dots.get(id)?.remove();
      dots.delete(id);
      peerStates.delete(id);
    }
  if (list.length) {
    empty.remove();
  } else if (!empty.isConnected) $("peopleList").append(empty);
  list.forEach((p, i) => {
    let row = rows.get(p.id),
      dot = dots.get(p.id);
    if (!row) {
      row = make("div", "person-row");
      row.dataset.peerId = p.id;
      const avatar = make("span", `avatar tone-${i % 3}`, initials(p.name));
      const identity = make("div", "person-identity");
      identity.append(
        make("div", "person-name", p.name),
        make("div", "person-state", "Verbindung wird aufgebaut"),
      );
      const volume = make("div", "person-volume");
      volume.append(make("div", ""));
      const mute = make("button", "mute-person", "Stumm");
      mute.onclick = () => {
        voice.mutePeer(p.id);
        try {
          localStorage.setItem(
            "zwoice.mutes",
            JSON.stringify([...voice.mutedPeers].slice(-256)),
          );
        } catch {}
        renderPeers(peers);
      };
      row.append(
        avatar,
        identity,
        make("span", "person-distance"),
        volume,
        mute,
      );
      rows.set(p.id, row);
      $("peopleList").append(row);
      dot = make("div", "radar-person");
      dot.append(
        make("div", `avatar tone-${i % 3}`, initials(p.name)),
        make("span", ""),
      );
      dots.set(p.id, dot);
      $("radarPeers").append(dot);
    }
    row.querySelector(".person-name").textContent = p.name;
    const muted = voice.mutedPeers.has(p.id);
    row.querySelector(".person-distance").textContent =
      `${Math.round(p.distance)} m`;
    row.querySelector(".person-volume>div").style.width =
      `${muted ? 0 : Math.round(p.gain * 100)}%`;
    row.querySelector(".person-state").textContent = muted
      ? "Für dich stumm"
      : p.gain <= 0
        ? "Außer Hörweite"
        : demo
          ? "Demo-Teilnehmer"
          : peerStates.get(p.id) === "connected"
            ? "Verbunden"
            : peerStates.get(p.id) === "failed"
              ? "Verbindung gestört"
              : "Verbindet …";
    const button = row.querySelector("button");
    button.textContent = muted ? "Freigeben" : "Stumm";
    button.setAttribute("aria-pressed", String(muted));
    button.setAttribute(
      "aria-label",
      `${p.name} ${muted ? "freigeben" : "stummschalten"}`,
    );
    // Angle is deliberately schematic; Sauce position is only used for distance.
    const angle = [-1.05, -2.7, 0.1, 2.25, 1.1, 3.1][i % 6],
      radius = 55 + Math.min(60, p.distance) * 1.35;
    dot.style.left = `calc(50% + ${Math.cos(angle) * radius}px)`;
    dot.style.top = `calc(47% + ${Math.sin(angle) * radius * 0.8}px)`;
    dot.style.opacity = p.gain <= 0 ? ".35" : "1";
    dot.lastElementChild.textContent = `${p.name.split(" ")[0]} · ${Math.round(p.distance)} m`;
  });
  $("peerCount").textContent = list.length;
  $("navCount").textContent = list.length;
  $("radarCaption").textContent = list.length
    ? "In Hörweite · schematische Ansicht ohne Fahrtrichtung"
    : "Dein nächstes Gespräch ist nur eine Radlänge entfernt.";
}
function updateAudioStatus(status) {
  if (status.meter !== undefined) {
    $("micMeter").style.width = `${status.meter * 100}%`;
    return;
  }
  if (status.peer) {
    peerStates.set(status.peer, status.state);
    renderPeers(peers);
  }
  if (status.rtt !== undefined)
    $("latency").textContent = status.rtt
      ? `${Math.round(status.rtt)} ms`
      : "—";
  if (status.speaking)
    for (const [id, row] of rows) {
      const speaking = status.speaking.includes(id);
      row.querySelector(".avatar").classList.toggle("speaking", speaking);
      dots.get(id)?.firstElementChild.classList.toggle("speaking", speaking);
    }
}
$("demoButton").onclick = () => {
  if (demo) {
    exitDemo();
    return;
  }
  stop();
  demo = true;
  $("demoButton").textContent = "Demo beenden";
  $("demoControl").hidden = false;
  badge("Demo · simulierte Fahrt", true);
  $("worldLabel").textContent = "Watopia · Demo";
  $("selfLabel").textContent = "Du · Demo";
  $("liveDot").classList.add("active");
  connection("sauce", false, "Demo · keine Verbindung");
  connection("server", false, "Demo · keine Sprachübertragung");
  notice(
    "Du siehst eine simulierte Fahrt. Verändere den Abstand zur Gruppe, um das Ausblenden zu erleben.",
  );
  renderDemo();
  renderControls();
};
function renderDemo() {
  const offset = Number($("demoDistance").value);
  $("demoDistanceValue").value = `${offset} m`;
  peers = [
    { id: "demo-flo", name: "Flo Weber", distance: 8 + offset },
    { id: "demo-emma", name: "Emma Klein", distance: 24 + offset },
    { id: "demo-nils", name: "Nils Berg", distance: 48 + offset },
  ].map((p) => ({ ...p, gain: distanceGain(p.distance) }));
  renderPeers(peers);
}
$("demoDistance").oninput = renderDemo;
function exitDemo() {
  demo = false;
  $("demoButton").textContent = "Demo erkunden ↗";
  $("demoControl").hidden = true;
  stop();
}
renderControls();
