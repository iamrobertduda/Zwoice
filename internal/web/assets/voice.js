// Native browser WebRTC + Web Audio. No framework, media server SDK or recorder.
export function distanceGain(distance) {
  if (!Number.isFinite(distance) || distance >= 60) return 0;
  if (distance <= 10) return 1;
  const t = (distance - 10) / 50;
  return 1 - t * t * (3 - 2 * t);
}

export function opusSDP(sdp) {
  const opus = sdp.match(/^a=rtpmap:(\d+) opus\/48000/m)?.[1];
  if (!opus) return sdp;
  const pattern = new RegExp(`^a=fmtp:${opus} (.*)$`, "m");
  const old = sdp.match(pattern);
  const params = new Map(
    (old?.[1] || "")
      .trim()
      .split(";")
      .filter(Boolean)
      .map((p) => p.trim().split("=")),
  );
  for (const [k, v] of Object.entries({
    useinbandfec: "1",
    usedtx: "1",
    stereo: "0",
    maxaveragebitrate: "32000",
  }))
    params.set(k, v);
  const line = `a=fmtp:${opus} ${[...params].map(([k, v]) => `${k}=${v}`).join(";")}`;
  return old
    ? sdp.replace(pattern, line + (old[0].endsWith("\r") ? "\r" : ""))
    : sdp.replace(
        new RegExp(`(^a=rtpmap:${opus}[^\\r\\n]+)`, "m"),
        `$1\r\n${line}`,
      );
}

function candidateMatches(candidate, description) {
  return (
    description &&
    (!candidate.usernameFragment ||
      description.sdp
        .split(/\r?\n/)
        .includes(`a=ice-ufrag:${candidate.usernameFragment}`))
  );
}

export class Voice {
  constructor(send, onStatus, onError) {
    this.send = send;
    this.onStatus = onStatus;
    this.onError = onError;
    this.peers = new Map();
    this.mutedPeers = new Set();
    this.generation = 0;
    this.muted = false;
    this.deafened = false;
    this.volume = 0.8;
    this.pushToTalk = false;
    this.pressed = false;
    this.allowed = false;
  }

  async open(settings) {
    const generation = ++this.generation;
    this.settings = settings;
    this.pushToTalk = settings.pushToTalk;
    this.pressed = false;
    this.context = new AudioContext({ latencyHint: "interactive" });
    await this.context.resume();
    this.master = this.context.createGain();
    this.compressor = this.context.createDynamicsCompressor();
    this.compressor.threshold.value = -6;
    this.compressor.ratio.value = 4;
    this.destination = this.context.createMediaStreamDestination();
    this.master.connect(this.compressor).connect(this.destination);
    this.output = new Audio();
    this.output.autoplay = true;
    this.output.srcObject = this.destination.stream;
    if (settings.outputDevice && this.output.setSinkId)
      await this.output.setSinkId(settings.outputDevice);
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        deviceId: settings.inputDevice
          ? { exact: settings.inputDevice }
          : undefined,
        channelCount: 1,
        echoCancellation: true,
        noiseSuppression: settings.noiseSuppression,
        autoGainControl: true,
      },
      video: false,
    });
    if (generation !== this.generation) {
      stream.getTracks().forEach((t) => t.stop());
      return false;
    }
    this.stream = stream;
    this.stream.getAudioTracks()[0].onended = () =>
      this.onError(
        "Mikrofon getrennt. Bitte Gerät prüfen und die Fahrt neu starten.",
        true,
      );
    this.analyser = this.context.createAnalyser();
    this.analyser.fftSize = 256;
    this.micSource = this.context.createMediaStreamSource(stream);
    this.micSource.connect(this.analyser);
    this.samples = new Uint8Array(256);
    await this.output.play();
    this.updateControls();
    this.statsTimer = setInterval(() => this.stats(), 1000);
    this.meterTimer = setInterval(() => this.meter(), 200);
    return true;
  }

  configure(id, iceServers) {
    this.id = id;
    this.iceServers = iceServers;
    if (
      this.settings.relayOnly &&
      !iceServers.some((s) => [].concat(s.urls).some((u) => /^turns?:/.test(u)))
    ) {
      throw new Error("IP-Schutz benötigt einen konfigurierten TURN-Dienst.");
    }
    for (const p of this.peers.values()) {
      p.pc.setConfiguration(this.configuration());
      // New credentials must replace the active TURN allocation before expiry.
      if (this.id < p.id) this.offer(p, true);
    }
  }
  configuration() {
    return {
      iceServers: this.iceServers,
      iceTransportPolicy: this.settings.relayOnly ? "relay" : "all",
      bundlePolicy: "max-bundle",
    };
  }
  updateControls() {
    if (this.stream)
      for (const track of this.stream.getAudioTracks())
        track.enabled =
          this.allowed &&
          !this.muted &&
          !this.deafened &&
          (!this.pushToTalk || this.pressed);
    if (this.context && this.master)
      this.master.gain.setTargetAtTime(
        this.deafened ? 0 : this.volume,
        this.context.currentTime,
        0.04,
      );
    for (const p of this.peers.values()) this.applyGain(p);
  }
  applyGain(p) {
    if (p.gain && this.context)
      p.gain.gain.setTargetAtTime(
        this.allowed && !this.mutedPeers.has(p.id) ? p.level : 0,
        this.context.currentTime,
        0.12,
      );
  }
  mutePeer(id) {
    this.mutedPeers.has(id)
      ? this.mutedPeers.delete(id)
      : this.mutedPeers.add(id);
    const p = this.peers.get(id);
    if (p) this.applyGain(p);
  }
  setAllowed(value) {
    this.allowed = value;
    this.updateControls();
    if (!value) this.clearPeers();
  }
  sync(peers) {
    if (!this.stream || !this.id || !this.allowed) return;
    const ids = new Set(peers.slice(0, 6).map((p) => p.id));
    for (const id of this.peers.keys()) if (!ids.has(id)) this.remove(id);
    for (const item of peers.slice(0, 6)) {
      const p = this.peers.get(item.id) || this.create(item.id);
      p.level = Math.max(0, Math.min(1, item.gain));
      this.applyGain(p);
    }
  }
  create(id) {
    const pc = new RTCPeerConnection(this.configuration());
    const p = {
      id,
      pc,
      level: 0,
      queue: Promise.resolve(),
      candidates: [],
      retry: 0,
      lastRestart: Date.now(),
      closed: false,
    };
    this.peers.set(id, p);
    pc.addTrack(this.stream.getAudioTracks()[0], this.stream);
    const opus = RTCRtpSender.getCapabilities?.("audio")?.codecs.filter(
      (c) => c.mimeType.toLowerCase() === "audio/opus",
    );
    if (opus?.length && pc.getTransceivers()[0]?.setCodecPreferences)
      pc.getTransceivers()[0].setCodecPreferences(opus);
    pc.onicecandidate = ({ candidate }) => {
      if (candidate && !p.closed)
        this.send({
          type: "signal",
          to: id,
          payload: { candidate: candidate.toJSON() },
        });
    };
    pc.ontrack = ({ track, streams }) => {
      if (p.closed) return;
      p.source?.disconnect();
      p.gain?.disconnect();
      p.source = this.context.createMediaStreamSource(
        streams[0] || new MediaStream([track]),
      );
      p.gain = this.context.createGain();
      p.gain.gain.value = 0;
      p.source.connect(p.gain).connect(this.master);
      this.applyGain(p);
    };
    pc.onconnectionstatechange = () => {
      if (p.closed) return;
      this.onStatus({ peer: id, state: pc.connectionState });
      if (pc.connectionState === "connected") {
        p.retry = 0;
        clearTimeout(p.retryTimer);
        p.retryTimer = null;
      }
      if (
        ["failed", "disconnected"].includes(pc.connectionState) &&
        !p.retryTimer
      ) {
        p.retryTimer = setTimeout(() => {
          p.retryTimer = null;
          if (p.closed || pc.connectionState === "connected") return;
          if (this.id < id) {
            p.retry++;
            p.lastRestart = Date.now();
            this.offer(p, true);
          } else
            this.onError(
              "Eine Sprachverbindung ist gestört. Prüfe Netzwerk und TURN-Konfiguration.",
            );
        }, 2500);
      }
    };
    if (this.id < id) this.offer(p);
    return p;
  }
  enqueue(p, task) {
    p.queue = p.queue
      .then(() => {
        if (!p.closed) return task();
      })
      .catch(() => {
        if (!p.closed)
          this.onError("Sprachverbindung konnte nicht ausgehandelt werden.");
      });
  }
  offer(p, restart = false) {
    this.enqueue(p, async () => {
      if (restart && p.pc.signalingState === "have-local-offer")
        await p.pc.setLocalDescription({ type: "rollback" });
      if (p.pc.signalingState !== "stable") return;
      const offer = await p.pc.createOffer({ iceRestart: restart });
      offer.sdp = opusSDP(offer.sdp);
      await p.pc.setLocalDescription(offer);
      if (!p.closed)
        this.send({
          type: "signal",
          to: p.id,
          payload: { description: p.pc.localDescription.toJSON() },
        });
    });
  }
  signal(from, payload) {
    const p = this.peers.get(from);
    if (!p || p.closed || !payload) return;
    this.enqueue(p, async () => {
      const description = payload.description;
      if (description) {
        if (!["offer", "answer"].includes(description.type)) return;
        // One deterministic offerer per pair prevents glare on simultaneous join.
        if (description.type === "offer" && this.id < from) return;
        if (
          description.type === "answer" &&
          p.pc.signalingState !== "have-local-offer"
        )
          return;
        await p.pc.setRemoteDescription(description);
        // ICE from a lost/replaced offer must not abort the new answer.
        for (const c of p.candidates.splice(0))
          if (candidateMatches(c, description))
            await p.pc.addIceCandidate(c).catch(() => {});
        if (description.type === "offer") {
          const answer = await p.pc.createAnswer();
          answer.sdp = opusSDP(answer.sdp);
          await p.pc.setLocalDescription(answer);
          if (!p.closed)
            this.send({
              type: "signal",
              to: from,
              payload: { description: p.pc.localDescription.toJSON() },
            });
        }
        const sender = p.pc.getSenders().find((s) => s.track?.kind === "audio");
        if (sender) {
          const params = sender.getParameters();
          if (params.encodings?.length) {
            params.encodings[0].maxBitrate = 32000;
            await sender.setParameters(params).catch(() => {});
          }
        }
      } else if (payload.candidate) {
        if (candidateMatches(payload.candidate, p.pc.remoteDescription))
          await p.pc.addIceCandidate(payload.candidate).catch(() => {});
        else if (p.candidates.length < 64) p.candidates.push(payload.candidate);
      }
    });
  }
  async stats() {
    if (this.statsBusy) return;
    this.statsBusy = true;
    try {
      let rtt = 0;
      const speaking = [];
      await Promise.all(
        [...this.peers.values()].map(async (p) => {
          const state = p.pc.connectionState;
          const stalled =
            ["new", "connecting"].includes(state) &&
            Date.now() - p.lastRestart > 15000;
          if (
            this.id < p.id &&
            (stalled || ["failed", "disconnected"].includes(state)) &&
            Date.now() - p.lastRestart >
              Math.min(30000, 5000 * 2 ** Math.min(p.retry, 3))
          ) {
            p.lastRestart = Date.now();
            p.retry++;
            this.offer(p, true);
          }
          const stats = await p.pc.getStats();
          for (const report of stats.values()) {
            if (
              report.type === "candidate-pair" &&
              report.state === "succeeded" &&
              report.nominated &&
              report.currentRoundTripTime
            )
              rtt = Math.max(rtt, report.currentRoundTripTime * 1000);
            if (
              report.type === "inbound-rtp" &&
              report.kind === "audio" &&
              report.audioLevel > 0.02 &&
              p.level > 0 &&
              !this.mutedPeers.has(p.id)
            )
              speaking.push(p.id);
          }
        }),
      );
      if (this.stream) this.onStatus({ rtt, speaking });
    } catch {
      /* peers may close during the stats snapshot */
    } finally {
      this.statsBusy = false;
    }
  }
  meter() {
    if (document.hidden || !this.analyser) return;
    this.analyser.getByteTimeDomainData(this.samples);
    let power = 0;
    for (const x of this.samples) power += ((x - 128) / 128) ** 2;
    this.onStatus({
      meter: this.stream?.getAudioTracks()[0]?.enabled
        ? Math.min(1, Math.sqrt(power / this.samples.length) * 5)
        : 0,
    });
  }
  remove(id) {
    const p = this.peers.get(id);
    if (!p) return;
    p.closed = true;
    clearTimeout(p.retryTimer);
    p.pc.close();
    p.source?.disconnect();
    p.gain?.disconnect();
    this.peers.delete(id);
  }
  clearPeers() {
    for (const id of this.peers.keys()) this.remove(id);
  }
  close() {
    ++this.generation;
    clearInterval(this.statsTimer);
    clearInterval(this.meterTimer);
    this.allowed = false;
    this.clearPeers();
    this.stream?.getTracks().forEach((t) => {
      t.onended = null;
      t.stop();
    });
    this.stream = null;
    this.micSource?.disconnect();
    this.analyser?.disconnect();
    this.analyser = null;
    if (this.output) {
      this.output.pause();
      this.output.srcObject = null;
      this.output = null;
    }
    this.destination?.stream.getTracks().forEach((t) => t.stop());
    this.destination = null;
    this.context?.close().catch(() => {});
    this.context = null;
    this.master = null;
    this.id = null;
  }
}
