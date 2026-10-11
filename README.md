# Zwoice

**Zusammen in Hörweite.** Plattformübergreifender Proximity-Voice-Chat für Zwift, mit Positionsdaten aus Sauce for Zwift.

Zwoice besteht aus einem kleinen Go-Programm und einer Oberfläche im vorhandenen Browser. Es bündelt weder Chromium noch eine Node-Runtime. Der Browser übernimmt seine optimierte WebRTC-Audioverarbeitung. Der Go-Prozess verbindet die lokale Sauce-API mit dem Signalisierungsdienst auf Railway. Sprache läuft verschlüsselt direkt zwischen Fahrern, bei Bedarf über TURN.

## Benutzen

1. Paket für dein Betriebssystem entpacken und Zwoice starten. Die Oberfläche öffnet sich automatisch.
2. Zwift und Sauce for Zwift starten; den Sauce-Webserver aktivieren.
3. In Zwoice deinen Anzeigenamen, die Serveradresse und deine persönliche Einladung eintragen.
4. **Fahrt starten**, Mikrofon freigeben und losfahren. Den Tab geöffnet lassen; Minimieren ist möglich.

Eine Einladung bindet deine Sitzung an die von Sauce gemeldete eigene Zwift-ID. Der Betreiber erstellt sie mit `zwoice invite`. Teilnehmer einer Einladungsgruppe hören sich automatisch, sobald sie auch in derselben Zwift-Welt und Event-Gruppe unterwegs sind. Dafür sind weder Zwift-Passwörter noch ein Zwift-Kontozugriff durch Zwoice nötig.

Die **Demo** ist eine ausdrücklich gekennzeichnete Vorschau. Sie benötigt weder Sauce noch Mikrofon und überträgt keine Sprache.

## Funktionen

- Eigene Position über das tatsächliche Sauce-Abonnement `athlete/self/v2`, nicht über den beobachteten Kamerafahrer.
- Dreidimensionale Distanz; volle Lautstärke bis 10 m, sanftes Ausblenden bis 60 m. Bestehende Verbindungen bleiben bis 75 m stumm bereit, um ständiges Neuverbinden zu vermeiden.
- Höchstens sechs Sprachpartner pro Fahrer, symmetrische Verbindungen und räumliche Indizierung auf dem Server.
- Opus mit 32 kbit/s Obergrenze pro Sender/Verbindung, DTX und FEC in der Aushandlung; Mono, Echounterdrückung, Geräuschunterdrückung und automatische Pegelregelung nach Browser-Unterstützung.
- Mikrofonwahl, Ausgabegerät nach Browser-Unterstützung, Gesamtlautstärke, Mikrofon stumm, einzelne Fahrer stumm, Audio pausieren und Push-to-talk bei Fensterfokus.
- Automatisches Wiederverbinden mit Backoff; Stummschalten bei veralteten Positionsdaten, Verbindungsverlust oder Geräteproblemen.
- Optionale TURN-only-Verbindungen zum Schutz der IP-Adresse gegenüber anderen Fahrern.
- Kurzlebige TURN-Zugangsdaten aus Cloudflare oder coturn, mit Erneuerung während langer Sitzungen.
- Keine Audioaufzeichnung, Analyse-Skripte, externen Schriftarten oder persistent gespeicherten Positionsverläufe.

## Lokal entwickeln

Go 1.26.3 oder neuer:

```sh
go run ./cmd/zwoice
```

Die Oberfläche liegt unter `http://127.0.0.1:8787`. Für einen zweiten lokalen Client: `go run ./cmd/zwoice -port 8788`. `-open=false` unterdrückt das automatische Browserfenster.

Lokaler Server in einem weiteren Terminal:

```sh
export ZWOICE_SIGNING_KEY="$(openssl rand -hex 32)"
go run ./cmd/zwoice server
```

In derselben Umgebung mit demselben Schlüssel eine Einladung erstellen:

```sh
go run ./cmd/zwoice invite -athlete 123456 -group public -valid 720h
```

`123456` durch die echte eigene Zwift-ID ersetzen. Die Ausgabe ist ein persönliches Zugangstoken. Die Oberfläche erhält als Serveradresse `http://127.0.0.1:8080`; entfernte Server verlangen HTTPS. Ohne konfiguriertes TURN können restriktive Netzwerke keine Sprache verbinden.

## Prüfen und Pakete bauen

```sh
go vet ./...
go test -race ./...
npm ci
npm test
npx playwright install chromium
npm run test:browser
node scripts/release.mjs 1.0.0
```

Node/Playwright werden ausschließlich für Entwicklung und Tests benötigt. Die Browser-Integrationstests starten zwei vollständige lokale Clients, einen echten Signalisierungsserver und eine kontrollierte Sauce-Quelle. Sie prüfen tatsächliche WebRTC-Audiopakete, Stummschaltung, Distanzänderung, Weltwechsel, veraltete Daten, Server-Neustart und Freigabe der Audiogeräte.

Die Paketierung erzeugt Windows-, macOS- und Linux-Dateien für AMD64 und ARM64 sowie SHA-256-Prüfsummen. macOS-Pakete enthalten eine `.app`; ohne Entwicklerzertifikat ist ihre lokale Signatur keine Apple-Notarisierung. Windows-Pakete sind ohne bereitgestelltes Zertifikat unsigniert.

## Betrieb und technische Entscheidungen

- [Installation und Bedienung](docs/INSTALL.md)
- [Railway und Cloudflare TURN](docs/DEPLOYMENT.md)
- [Architektur, Grenzen und Skalierung](docs/ARCHITECTURE.md)
- [Geprüfte Sauce-Schnittstelle](docs/SAUCE.md)
- [Daten und Sicherheitsmodell](docs/SECURITY.md)
- [Freigabeprüfungen und Messergebnisse](docs/VALIDATION.md)

Repository: [iamrobertduda/Zwoice](https://github.com/iamrobertduda/Zwoice). Zwoice ist ein unabhängiges Projekt und nicht mit Zwift oder Sauce LLC verbunden.
