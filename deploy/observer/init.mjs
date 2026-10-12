// Configure the independently installed Sauce application's inspected settings
// schema. No Sauce application code is imported, copied or modified here.
import { mkdir, chmod } from "node:fs/promises";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
const [home, rawPort] = process.argv.slice(2);
const port = Number(rawPort);
if (!home || !Number.isInteger(port) || port < 1024 || port > 65534) {
  throw new Error("Invalid observer runtime settings");
}
const directory = join(home, ".sauce4zwift");
await mkdir(directory, { recursive: true, mode: 0o700 });
const path = join(directory, "storage.sqlite");
const db = new DatabaseSync(path);
try {
  db.exec("PRAGMA journal_mode = WAL; CREATE TABLE IF NOT EXISTS store (id TEXT PRIMARY KEY, data TEXT)");
  const existing = db.prepare("SELECT data FROM store WHERE id = ?").get("app-settings");
  const settings = existing ? JSON.parse(existing.data) : {};
  Object.assign(settings, {webServerEnabled: true, webServerPort: port, gameConnectionEnabled: false});
  db.prepare("INSERT OR REPLACE INTO store (id, data) VALUES (?, ?)").run("app-settings", JSON.stringify(settings));
} finally { db.close(); }
await chmod(path, 0o600);
