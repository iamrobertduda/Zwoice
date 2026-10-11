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
              sauce: `http://127.0.0.1:${11081 + i}`,
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

test("first launch needs no account, invitation, name or settings and waits for the ride", async ({
  page,
  request,
}) => {
  const config = await (
    await request.get("http://127.0.0.1:11080/config")
  ).json();
  await page.route("**/config.json", (route) =>
    route.fulfill({ json: { ...config, sauce: "http://127.0.0.1:11081" } }),
  );
  await request.post("http://127.0.0.1:11080/move?id=1001&riding=false");
  await page.addInitScript(() => {
    window.__sockets = [];
    const WS = WebSocket;
    window.WebSocket = class extends WS {
      constructor(...args) {
        super(...args);
        window.__sockets.push(this);
      }
    };
  });
  try {
    await page.goto("http://127.0.0.1:18787");
    await expect(page.locator("#setupTitle")).toHaveText(
      "Sauce ist da. Jetzt noch losfahren.",
      { timeout: 8000 },
    );
    await expect(page.locator("#inviteToken")).toHaveCount(0);
    await page.getByRole("button", { name: "Fahrt starten" }).click();
    await expect(page.locator("#settingsDialog")).not.toBeVisible();
    // Longer than the heartbeat watchdog: waiting must not restart the session.
    await expect
      .poll(async () => page.evaluate(() => window.__sockets.length))
      .toBe(1);
    await page.waitForTimeout(8200);
    expect(await page.evaluate(() => window.__sockets.length)).toBe(1);
    await request.post("http://127.0.0.1:11080/move?id=1001&riding=true");
    await expect(page.locator("#sessionBadge")).toHaveText(
      "Du bist in Hörweite",
      { timeout: 15000 },
    );
    await expect(page.locator("#setupPanel")).not.toBeVisible();
    await page.getByRole("button", { name: "Fahrt beenden" }).click();
    await expect(page.locator("#setupTitle")).toHaveText("Bereit, Flo?", {
      timeout: 8000,
    });
    await page.locator("#settingsNav").click();
    await expect(page.locator("#serverURL")).not.toBeVisible();
    await expect(page.locator("#displayName")).toHaveValue("");
    await page.keyboard.press("Escape");
    await page.screenshot({
      path: "test-results/zwoice-ready.png",
      fullPage: true,
    });
  } finally {
    await request.post("http://127.0.0.1:11080/move?id=1001&riding=true");
  }
});

test("missing Sauce gives an actionable prompt and upgrades discard old invitations", async ({
  page,
}) => {
  await page.addInitScript(() =>
    localStorage.setItem(
      "zwoice.settings",
      JSON.stringify({
        token: "old-secret",
        remember: true,
        sauce: "http://127.0.0.1:11079",
      }),
    ),
  );
  await page.goto("http://127.0.0.1:18787");
  await expect(page.locator("#sauceActions")).toBeVisible({ timeout: 8000 });
  await expect(
    page.getByRole("link", { name: "Sauce herunterladen ↗" }),
  ).toHaveAttribute(
    "href",
    "https://www.sauce.llc/products/sauce4zwift/download",
  );
  expect(
    await page.evaluate(() => localStorage.getItem("zwoice.settings")),
  ).not.toContain("old-secret");
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/zwoice-setup-mobile.png",
    fullPage: true,
  });
});

test("public download page offers one installer per desktop platform", async ({
  page,
}) => {
  await page.goto("http://127.0.0.1:18089");
  await expect(page.getByRole("heading", { level: 1 })).toContainText(
    "Dein nächstes Gespräch",
  );
  await page.locator("#platform").selectOption("macos");
  await expect(page.locator("#downloadButton")).toHaveAttribute(
    "href",
    /Zwoice-macOS\.dmg$/,
  );
  await page.locator("#platform").selectOption("windows");
  await expect(page.locator("#downloadButton")).toHaveAttribute(
    "href",
    /Zwoice-Windows-Setup\.exe$/,
  );
  await page.screenshot({
    path: "test-results/zwoice-downloads.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/zwoice-downloads-mobile.png",
    fullPage: true,
  });
});
