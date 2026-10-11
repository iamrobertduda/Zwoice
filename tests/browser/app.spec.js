import { test, expect } from "@playwright/test";

test("responsive demo, distance fading, mutes and keyboard-accessible setup", async ({
  page,
}) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("http://127.0.0.1:18787");
  await expect(
    page.getByRole("heading", { name: "Zusammen in Hörweite." }),
  ).toBeVisible();
  await page.getByRole("button", { name: /Demo erkunden/ }).click();
  await expect(page.locator(".person-row")).toHaveCount(3);
  await page.getByRole("button", { name: "Flo Weber stummschalten" }).click();
  await expect(
    page.getByRole("button", { name: "Flo Weber freigeben" }),
  ).toHaveAttribute("aria-pressed", "true");
  await page.locator("#demoDistance").fill("65");
  await page.locator("#demoDistance").dispatchEvent("input");
  await expect(
    page.locator(".person-state").filter({ hasText: "Außer Hörweite" }),
  ).toHaveCount(2);
  await page.locator("#demoDistance").fill("0");
  await page.locator("#demoDistance").dispatchEvent("input");
  await page.screenshot({
    path: "test-results/zwoice-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/zwoice-mobile.png",
    fullPage: true,
  });
  await page.locator("#settingsNav").click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).not.toBeVisible();
  expect(errors).toEqual([]);
});

test("two real WebRTC clients: Opus audio, controls, movement, stale source, reconnect and cleanup", async ({
  browser,
  request,
}) => {
  const contexts = await Promise.all([
    browser.newContext(),
    browser.newContext(),
  ]);
  const pages = await Promise.all(contexts.map((c) => c.newPage()));
  const errors = [];
  pages.forEach((p) => p.on("pageerror", (e) => errors.push(e.message)));
  try {
    for (let i = 0; i < 2; i++) {
      const id = 1001 + i,
        config = await (
          await request.get(`http://127.0.0.1:11080/config?id=${id}`)
        ).json();
      await pages[i].addInitScript(
        ({ i, config, relayOnly, turnTLS }) => {
          localStorage.setItem(
            "zwoice.settings",
            JSON.stringify({
              ...config,
              name: i ? "Emma Klein" : "Flo Weber",
              sauce: `http://127.0.0.1:${11081 + i}`,
              remember: true,
              volume: 80,
              relayOnly,
            }),
          );
          window.__connections = [];
          window.__streams = [];
          window.__gains = [];
          const RTC = window.RTCPeerConnection;
          window.RTCPeerConnection = class extends RTC {
            constructor(configuration) {
              if (turnTLS)
                configuration = {
                  ...configuration,
                  iceServers: configuration.iceServers
                    .map((s) => ({
                      ...s,
                      urls: []
                        .concat(s.urls)
                        .filter((u) => u.startsWith("turns:")),
                    }))
                    .filter((s) => s.urls.length),
                };
              super(configuration);
              window.__connections.push(this);
            }
          };
          if (i === 0 && !relayOnly) {
            // A lost initial offer must recover without a source change or user action.
            let drop = true;
            const send = WebSocket.prototype.send;
            WebSocket.prototype.send = function (data) {
              const m = JSON.parse(data);
              if (
                drop &&
                m.type === "signal" &&
                m.payload?.description?.type === "offer"
              ) {
                drop = false;
                return;
              }
              return send.call(this, data);
            };
          }
          const gum = navigator.mediaDevices.getUserMedia.bind(
            navigator.mediaDevices,
          );
          navigator.mediaDevices.getUserMedia = async (...args) => {
            const s = await gum(...args);
            window.__streams.push(s);
            return s;
          };
          const gain = AudioContext.prototype.createGain;
          AudioContext.prototype.createGain = function () {
            const g = gain.call(this);
            window.__gains.push(g);
            return g;
          };
        },
        {
          i,
          config,
          relayOnly: Boolean(process.env.ZWOICE_TEST_SERVER),
          turnTLS: Boolean(process.env.ZWOICE_TEST_TURN_TLS),
        },
      );
      await pages[i].goto(`http://127.0.0.1:${18787 + i}`);
      await pages[i].getByRole("button", { name: "Fahrt starten" }).click();
    }
    const [a, b] = pages;
    for (const p of pages) {
      await expect(p.locator(".person-state")).toHaveText("Verbunden", {
        timeout: 35000,
      });
    }
    const bytes = async (p) =>
      p.evaluate(async () => {
        let n = 0;
        for (const pc of window.__connections) {
          if (pc.connectionState === "closed") continue;
          const reports = await pc.getStats();
          for (const r of reports.values())
            if (r.type === "inbound-rtp" && r.kind === "audio")
              n += r.bytesReceived || 0;
        }
        return n;
      });
    await expect.poll(() => bytes(a)).toBeGreaterThan(100);
    await expect.poll(() => bytes(b)).toBeGreaterThan(100);
    if (process.env.ZWOICE_TEST_SERVER) {
      for (const p of pages) {
        const candidates = await p.evaluate(async () => {
          const stats = await window.__connections.at(-1).getStats();
          const transport = [...stats.values()].find(
            (r) => r.type === "transport" && r.selectedCandidatePairId,
          );
          const pair = stats.get(transport?.selectedCandidatePairId);
          const local = stats.get(pair?.localCandidateId);
          return {
            local: local?.candidateType,
            remote: stats.get(pair?.remoteCandidateId)?.candidateType,
            relayProtocol: local?.relayProtocol,
          };
        });
        expect(candidates).toMatchObject({ local: "relay", remote: "relay" });
        if (process.env.ZWOICE_TEST_TURN_TLS)
          expect(candidates.relayProtocol).toBe("tls");
      }
      console.log(
        `Hosted Railway + Cloudflare: bidirectional Opus audio; selected candidates relay/relay${process.env.ZWOICE_TEST_TURN_TLS ? "; TURN over TLS" : ""}.`,
      );
    }
    expect(
      await a.evaluate(() => window.__connections.at(-1).localDescription.sdp),
    ).toContain("opus/48000");
    await a.locator("#micButton").click();
    expect(
      await a.evaluate(
        () => window.__streams.at(-1).getAudioTracks()[0].enabled,
      ),
    ).toBe(false);
    await a.locator("#micButton").click();
    expect(
      await a.evaluate(
        () => window.__streams.at(-1).getAudioTracks()[0].enabled,
      ),
    ).toBe(true);
    await a.locator("#deafenButton").click();
    expect(
      await a.evaluate(
        () => window.__streams.at(-1).getAudioTracks()[0].enabled,
      ),
    ).toBe(false);
    await a.locator("#deafenButton").click();
    await a.getByRole("button", { name: "Emma Klein stummschalten" }).click();
    await expect
      .poll(() => a.evaluate(() => window.__gains.at(-1).gain.value))
      .toBeLessThan(0.01);
    await a.getByRole("button", { name: "Emma Klein freigeben" }).click();
    await request.post("http://127.0.0.1:11080/move?id=1002&x=70");
    await expect(a.locator(".person-state")).toHaveText("Außer Hörweite");
    await expect
      .poll(() => a.evaluate(() => window.__gains.at(-1).gain.value))
      .toBeLessThan(0.01);
    await request.post("http://127.0.0.1:11080/move?id=1002&x=90");
    await expect(a.locator(".person-row")).toHaveCount(0);
    await request.post("http://127.0.0.1:11080/move?id=1002&x=8");
    await expect(a.locator(".person-state")).toHaveText("Verbunden", {
      timeout: 15000,
    });
    await request.post("http://127.0.0.1:11080/move?id=1002&course=2");
    await expect(a.locator(".person-row")).toHaveCount(0);
    await request.post("http://127.0.0.1:11080/move?id=1002&course=1");
    await expect(a.locator(".person-state")).toHaveText("Verbunden", {
      timeout: 15000,
    });
    await request.post("http://127.0.0.1:11080/move?id=1002&frozen=true");
    await expect(b.locator("#sessionBadge")).toHaveText("Sprache pausiert", {
      timeout: 8000,
    });
    await expect(a.locator(".person-row")).toHaveCount(0);
    expect(
      await b.evaluate(
        () => window.__streams.at(-1).getAudioTracks()[0].enabled,
      ),
    ).toBe(false);
    await request.post("http://127.0.0.1:11080/move?id=1002&frozen=false");
    await expect(a.locator(".person-state")).toHaveText("Verbunden", {
      timeout: 15000,
    });
    if (!process.env.ZWOICE_TEST_SERVER) {
      const closed = await a.evaluate(
        () =>
          window.__connections.filter((p) => p.connectionState === "closed")
            .length,
      );
      await request.post("http://127.0.0.1:11080/reset");
      await expect
        .poll(() =>
          a.evaluate(
            () =>
              window.__connections.filter((p) => p.connectionState === "closed")
                .length,
          ),
        )
        .toBeGreaterThan(closed);
      await expect(a.locator(".person-state")).toHaveText("Verbunden", {
        timeout: 20000,
      });
    }
    await a.getByRole("button", { name: "Fahrt beenden" }).click();
    expect(
      await a.evaluate(() =>
        window.__streams.every((s) =>
          s.getTracks().every((t) => t.readyState === "ended"),
        ),
      ),
    ).toBe(true);
    expect(
      await a.evaluate(() =>
        window.__connections.every((p) => p.connectionState === "closed"),
      ),
    ).toBe(true);
    await expect(b.locator(".person-row")).toHaveCount(0);
    expect(errors).toEqual([]);
  } finally {
    await Promise.all(contexts.map((c) => c.close()));
    await request.post(
      "http://127.0.0.1:11080/move?id=1002&x=8&course=1&frozen=false",
    );
  }
});
