const base =
  "https://github.com/iamrobertduda/Zwoice/releases/download/v1.2.0/";
const platforms = {
  windows: {
    label: "Windows",
    file: "Zwoice-Windows-Setup.exe",
    note: "Für Intel, AMD und ARM. Keine Administratorrechte nötig.",
    install:
      "Öffne die heruntergeladene Datei. Der Installer legt Zwoice im Startmenü an und kann es direkt starten.",
  },
  macos: {
    label: "macOS",
    file: "Zwoice-macOS.dmg",
    note: "Ein Download für Apple Silicon und Intel. Ab macOS 12.",
    install:
      "Öffne die DMG-Datei, ziehe Zwoice nach Programme und öffne die App dort.",
  },
  linux: {
    label: "Linux",
    file: "zwoice-1.2.0-linux-amd64.tar.gz",
    note: "Für 64-Bit-PCs mit Intel- oder AMD-Prozessor.",
    install:
      "Entpacke das Archiv und starte die enthaltene Datei zwoice. Dein Browser öffnet sich automatisch.",
  },
  "linux-arm": {
    label: "Linux ARM64",
    file: "zwoice-1.2.0-linux-arm64.tar.gz",
    note: "Für Linux auf 64-Bit-ARM-Geräten.",
    install:
      "Entpacke das Archiv und starte die enthaltene Datei zwoice. Dein Browser öffnet sich automatisch.",
  },
};
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
function update() {
  const p = platforms[select.value];
  const link = document.getElementById("downloadButton");
  link.href = base + p.file;
  link.replaceChildren(
    document.createTextNode(`Für ${p.label} herunterladen `),
  );
  const arrow = document.createElement("span");
  arrow.textContent = "↓";
  arrow.setAttribute("aria-hidden", "true");
  link.append(arrow);
  document.getElementById("downloadNote").textContent = p.note;
  document.getElementById("installDetail").textContent = p.install;
}
select.addEventListener("change", update);
update();
