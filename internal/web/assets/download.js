const base =
  "https://github.com/iamrobertduda/Zwoice/releases/download/v1.3.0/";
const files = {
  windows: "Zwoice-Windows-Setup.exe",
  macos: "Zwoice-macOS.dmg",
  linux: "zwoice-1.3.0-linux-amd64.tar.gz",
  "linux-arm": "zwoice-1.3.0-linux-arm64.tar.gz",
};
const lang = document.documentElement.lang;
const select = document.getElementById("platform");
const os = navigator.userAgentData?.platform || navigator.platform || "";
select.value = /mac/i.test(os)
  ? "macos"
  : /linux/i.test(os)
    ? "linux"
    : "windows";
if (
  /iPhone|iPad|Android/i.test(navigator.userAgent) ||
  (/Mac/.test(os) && navigator.maxTouchPoints > 1)
)
  document.getElementById("mobileNote").hidden = false;
// Texts come from the server-rendered page in its language.
function update() {
  const option = select.selectedOptions[0];
  for (const link of document.querySelectorAll("[data-download]")) {
    link.href = base + files[select.value];
    link.replaceChildren(
      document.createTextNode(
        link.dataset.label.replace("{os}", option.textContent.trim()) + " ",
      ),
    );
    const arrow = document.createElement("span");
    arrow.textContent = "↓";
    arrow.setAttribute("aria-hidden", "true");
    link.append(arrow);
  }
  document.getElementById("downloadNote").textContent = option.dataset.note;
  document.getElementById("installDetail").textContent = option.dataset.install;
}
select.addEventListener("change", update);
update();

const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;

const header = document.querySelector(".site-header");
const onScroll = () => header.classList.toggle("is-scrolled", scrollY > 16);
addEventListener("scroll", onScroll, { passive: true });
onScroll();

if (!reduceMotion && "IntersectionObserver" in window) {
  document.documentElement.classList.add("motion");
  const reveal = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        entry.target.classList.add("is-visible");
        reveal.unobserve(entry.target);
      }
    },
    { rootMargin: "0px 0px -8% 0px", threshold: 0.12 },
  );
  for (const el of document.querySelectorAll(".reveal")) reveal.observe(el);
}

const scene = document.querySelector(".hero-scene");
if (!reduceMotion && matchMedia("(pointer: fine)").matches) {
  let frame = 0;
  let x = 0;
  let y = 0;
  addEventListener(
    "pointermove",
    (event) => {
      x = event.clientX / innerWidth - 0.5;
      y = event.clientY / innerHeight - 0.5;
      frame ||= requestAnimationFrame(() => {
        frame = 0;
        scene.style.setProperty("--mx", x.toFixed(3));
        scene.style.setProperty("--my", y.toFixed(3));
      });
    },
    { passive: true },
  );
}

// Same curve as distanceGain() in voice.js: full to 10 m, smooth fade to 60 m.
function distanceGain(distance) {
  if (distance >= 60) return 0;
  if (distance <= 10) return 1;
  const t = (distance - 10) / 50;
  return 1 - t * t * (3 - 2 * t);
}
const demo = document.getElementById("distanceDemo");
const range = document.getElementById("distanceRange");
const meters = new Intl.NumberFormat(lang, { style: "unit", unit: "meter" });
const metersLong = new Intl.NumberFormat(lang, {
  style: "unit",
  unit: "meter",
  unitDisplay: "long",
});
const percent = new Intl.NumberFormat(lang, { style: "percent" });
function renderDistance() {
  const distance = Number(range.value);
  const gain = distanceGain(distance);
  const zone =
    distance <= 10
      ? "full"
      : distance < 60
        ? "fade"
        : distance < 75
          ? "quiet"
          : "gone";
  const state = zone === "fade" ? (distance < 35 ? "near" : "far") : zone;
  demo.dataset.zone = zone;
  demo.style.setProperty("--pos", (distance / 75).toFixed(4));
  demo.style.setProperty("--gain", gain.toFixed(3));
  document.getElementById("distanceValue").textContent =
    meters.format(distance);
  document.getElementById("gainValue").textContent = percent.format(gain);
  document.getElementById("distanceState").textContent = demo.dataset[state];
  range.setAttribute(
    "aria-valuetext",
    demo.dataset.valuetext
      .replace("{d}", metersLong.format(distance))
      .replace("{p}", percent.format(gain)),
  );
}
range.addEventListener("input", renderDistance);
renderDistance();

// Until someone grabs the slider, Flo slowly drops back and closes in again.
if (!reduceMotion && "IntersectionObserver" in window) {
  let playing = false;
  let frame = 0;
  let start = 0;
  const tick = (now) => {
    start ||= now;
    const phase = ((now - start) / 11000) * Math.PI * 2;
    const distance = Math.round(32 - 30 * Math.cos(phase));
    if (Number(range.value) !== distance) {
      range.value = distance;
      renderDistance();
    }
    frame = requestAnimationFrame(tick);
  };
  const autoplay = new IntersectionObserver(([entry]) => {
    if (entry.isIntersecting && !playing) {
      playing = true;
      start = 0;
      frame = requestAnimationFrame(tick);
    } else if (!entry.isIntersecting && playing) {
      playing = false;
      cancelAnimationFrame(frame);
    }
  });
  autoplay.observe(demo);
  const stop = () => {
    autoplay.disconnect();
    playing = false;
    cancelAnimationFrame(frame);
  };
  for (const type of ["pointerdown", "keydown", "focus"])
    range.addEventListener(type, stop, { once: true });
}

const mic = document.getElementById("micDemo");
mic.addEventListener("click", () => {
  const muted = mic.getAttribute("aria-pressed") !== "true";
  mic.setAttribute("aria-pressed", String(muted));
  document.getElementById("audioPreview").classList.toggle("is-muted", muted);
  document.getElementById("micDemoStatus").textContent = muted
    ? mic.dataset.muted
    : mic.dataset.ready;
  document.getElementById("micDemoLabel").textContent = muted
    ? mic.dataset.off
    : mic.dataset.on;
  document.getElementById("micDemoHint").textContent = muted
    ? mic.dataset.hintOff
    : mic.dataset.hintOn;
});

const languageMenu = document.querySelector(".lang-menu");
document.addEventListener("click", (event) => {
  if (languageMenu.open && !languageMenu.contains(event.target))
    languageMenu.open = false;
});
languageMenu.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  languageMenu.open = false;
  languageMenu.querySelector("summary").focus();
});
