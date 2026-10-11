import {
  mkdirSync,
  cpSync,
  rmSync,
  symlinkSync,
  writeFileSync,
  copyFileSync,
  readFileSync,
  statSync,
  readdirSync,
  existsSync,
} from "node:fs";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { join, dirname } from "node:path";

const version = process.argv[2] || "dev";
const bundleVersion = version.match(/^v?(\d+\.\d+\.\d+)/)?.[1] || "0.0.0";
if (!/^[a-zA-Z0-9._-]+$/.test(version)) throw new Error("Invalid version");
mkdirSync("dist", { recursive: true });
for (const os of ["darwin", "windows", "linux"])
  for (const arch of ["arm64", "amd64"]) {
    const name = `zwoice-${version}-${os}-${arch}`,
      folder = join("dist", name);
    const binFolder =
      os === "darwin"
        ? join(folder, "Zwoice.app", "Contents", "MacOS")
        : folder;
    mkdirSync(binFolder, { recursive: true });
    const binary = join(binFolder, os === "windows" ? "Zwoice.exe" : "zwoice");
    const flags = `-s -w -X main.version=${version}${os === "windows" ? " -H windowsgui" : ""}`;
    execFileSync(
      "go",
      ["build", "-trimpath", "-ldflags", flags, "-o", binary, "./cmd/zwoice"],
      {
        env: { ...process.env, GOOS: os, GOARCH: arch, CGO_ENABLED: "0" },
        stdio: "inherit",
      },
    );
    if (os === "darwin") {
      const resources = join(folder, "Zwoice.app", "Contents", "Resources");
      mkdirSync(resources, { recursive: true });
      copyFileSync("assets/zwoice.icns", join(resources, "zwoice.icns"));
      writeFileSync(
        join(folder, "Zwoice.app", "Contents", "Info.plist"),
        `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleName</key><string>Zwoice</string><key>CFBundleDisplayName</key><string>Zwoice</string><key>CFBundleIdentifier</key><string>cc.zwoice.desktop</string><key>CFBundleExecutable</key><string>zwoice</string><key>CFBundleIconFile</key><string>zwoice.icns</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleVersion</key><string>${bundleVersion}</string><key>LSMinimumSystemVersion</key><string>12.0</string><key>LSUIElement</key><true/></dict></plist>`,
      );
      if (process.platform === "darwin")
        execFileSync("codesign", [
          "--force",
          "--deep",
          "--sign",
          process.env.MACOS_SIGN_IDENTITY || "-",
          join(folder, "Zwoice.app"),
        ]);
    }
    copyFileSync("docs/INSTALL.md", join(folder, "START-HERE.md"));
    copyFileSync(
      "THIRD_PARTY_NOTICES.md",
      join(folder, "THIRD_PARTY_NOTICES.md"),
    );
    const goRoot = execFileSync("go", ["env", "GOROOT"], {
      encoding: "utf8",
    }).trim();
    const goLicense = [
      join(goRoot, "LICENSE"),
      join(dirname(goRoot), "LICENSE"),
    ].find(existsSync);
    if (!goLicense) throw new Error("Go license not found");
    copyFileSync(goLicense, join(folder, "GO-LICENSE.txt"));
    if (os === "linux")
      execFileSync("tar", [
        "-czf",
        join("dist", `${name}.tar.gz`),
        "-C",
        "dist",
        name,
      ]);
    else execFileSync("zip", ["-qr", `${name}.zip`, name], { cwd: "dist" });
    console.log(
      `${name}: ${(statSync(binary).size / 1024 / 1024).toFixed(1)} MiB executable`,
    );
  }
// One macOS download, regardless of Intel or Apple Silicon. Finder exposes
// the usual app-to-Applications drag operation through the included symlink.
if (process.platform === "darwin") {
  const staging = join("dist", "macos-installer");
  rmSync(staging, { recursive: true, force: true });
  mkdirSync(staging, { recursive: true });
  const app = join(staging, "Zwoice.app");
  cpSync(join("dist", `zwoice-${version}-darwin-arm64`, "Zwoice.app"), app, {
    recursive: true,
  });
  execFileSync("lipo", [
    "-create",
    join(
      "dist",
      `zwoice-${version}-darwin-arm64`,
      "Zwoice.app",
      "Contents",
      "MacOS",
      "zwoice",
    ),
    join(
      "dist",
      `zwoice-${version}-darwin-amd64`,
      "Zwoice.app",
      "Contents",
      "MacOS",
      "zwoice",
    ),
    "-output",
    join(app, "Contents", "MacOS", "zwoice"),
  ]);
  execFileSync("codesign", [
    "--force",
    "--deep",
    "--sign",
    process.env.MACOS_SIGN_IDENTITY || "-",
    app,
  ]);
  symlinkSync("/Applications", join(staging, "Applications"));
  copyFileSync(
    "THIRD_PARTY_NOTICES.md",
    join(staging, "THIRD_PARTY_NOTICES.md"),
  );
  copyFileSync(
    join("dist", `zwoice-${version}-darwin-arm64`, "GO-LICENSE.txt"),
    join(staging, "GO-LICENSE.txt"),
  );
  execFileSync(
    "hdiutil",
    [
      "create",
      "-volname",
      "Zwoice",
      "-srcfolder",
      staging,
      "-ov",
      "-format",
      "UDZO",
      join("dist", "Zwoice-macOS.dmg"),
    ],
    { stdio: "inherit" },
  );
}
const checksums = readdirSync("dist")
  .filter(
    (n) =>
      (n.startsWith(`zwoice-${version}-`) && /\.(zip|gz)$/.test(n)) ||
      n === "Zwoice-macOS.dmg" ||
      n === "Zwoice-Windows-Setup.exe",
  )
  .map(
    (n) =>
      `${createHash("sha256")
        .update(readFileSync(join("dist", n)))
        .digest("hex")}  ${n}`,
  )
  .join("\n");
writeFileSync("dist/SHA256SUMS", checksums + "\n");
