# Prüfprotokoll und Freigabe

Stand: 11. Oktober 2026. Dieses Protokoll unterscheidet ausgeführte Prüfungen von noch offenen Geräte- und Feldtests. Gebaute Pakete und ein erfolgreicher Cloud-Test allein belegen keinen störungsfreien mehrstündigen Betrieb mit Zwift auf allen Plattformen.

## Ausgeführt

| Prüfung | Ergebnis und Umfang |
| --- | --- |
| Go-Tests mit Race Detector | Authentifizierung, Zeitablauf, Nähegraph, Gruppentrennung, räumliche Kapazität, Signalisierungsberechtigung, Sauce-Schema, lokale Host-/Origin-Prüfung bestanden. |
| Statische Prüfung | `go vet ./...`, JavaScript-Syntaxprüfung und Node-Tests bestanden. |
| Browser Ende-zu-Ende | Zwei vollständige Clients mit kontrollierter Sauce-Quelle. Tatsächliche empfangene Opus-Audiopakete, Mikrofon/Deafen/Teilnehmer-Mute, Abstand 8/70/90 m, Weltwechsel, eingefrorene Quelle, Wiederkehr und Freigabe aller Verbindungen/Audiotracks geprüft. |
| Lokaler Dienstneustart und verlorenes Startsignal | Automatisches Wiederverbinden nach Neustart des Testbrokers sowie nach absichtlich verworfenem ersten WebRTC-Offer einschließlich veralteter ICE-Kandidaten geprüft. |
| Railway + Cloudflare TURN | Öffentlicher Healthcheck HTTP 200. Zwei vollständige Clients übertragen bidirektional Opus-Audio. Beide Seiten erzwingen Relay-only; `getStats()` bestätigt ausgewählte Kandidaten `relay/relay`. Abstand, Weltwechsel, Ausfall/Rückkehr der Positionsquelle und Aufräumen auch über den gehosteten Dienst geprüft. Ein weiterer Durchlauf erzwingt ausschließlich TURN/TLS und bestätigt `relayProtocol=tls`. |
| Oberfläche | Desktop 1440 × 1060 und Mobilansicht 390 × 844 visuell geprüft; kein horizontaler Überlauf; Dialog und Escape-Taste geprüft. Mobile Ansicht ist kein nativer Mobilclient. |
| Lastlauf | 1.000 WebSocket-Clients, 90 Sekunden, 44.000 empfangene Nachrichten, keine unerwartete Trennung. |
| Paketierung | macOS, Windows und Linux jeweils ARM64/AMD64 gebaut. Ausführbare Dateien 6,4–7,1 MiB; Archive mit Prüfsummen. macOS ARM64 lokal gestartet. |

Zusätzlich ist der [GitHub-Prüflauf für Commit 38636f6](https://github.com/iamrobertduda/Zwoice/actions/runs/38120994517) vollständig erfolgreich: Go-Tests mit Race Detector unter Windows, macOS und Linux, Chromium-Integrationstests unter Linux und Paketierung aller sechs Architekturen. Das ist eine Laufzeitprüfung des Go-Kerns auf drei Betriebssystemen, noch keine Prüfung physischer Audiogeräte dort.

Der Cloud-Test verwendet synthetische Mikrofon-Audiodaten und Sauce-Positionsdaten nach dem untersuchten echten API-Schema. Er verwendet **echtes WebRTC, den produktiven Railway-Dienst und Cloudflare TURN**, aber keine reale Zwift-Fahrt und keine zwei verschiedenen Internetanschlüsse. Die simulierten Athleten verwenden eine isolierte Testgruppe und kurzlebige Einladungen. Dauerhafte Secrets werden dafür nur im Prozessspeicher gehalten; Browser-Traces sind im gehosteten Test deaktiviert.

## Ressourcenmessung

Apple M4, macOS ARM64, Go 1.26.3:

- Nähegraph für 1.000 Fahrer: ungefähr 6,59 ms pro Berechnung, 5,61 MB Allokationen pro Berechnung. Dies ist ein isolierter Go-Benchmark, keine gemessene Audio-Ende-zu-Ende-Latenz.
- Der Lastprozess mit Server **und allen 1.000 simulierten Clients im selben Prozess** nutzte am Ende 60,2 MiB Go-Heap und 4.003 Goroutinen. Nach Trennung und Garbage Collection: 2,9 MiB Heap und eine Goroutine. Diese Heapzahl ist weder Prozess-RSS noch ein Speicherbedarf pro produktivem Server oder Desktop-Client.
- Der Browser gehört zum Ressourcenverbrauch des Desktop-Clients. Die kleine Binärdatei belegt keine RAM-/CPU-Obergrenze einschließlich Browser, sechs Audioströmen und Zwift.
- Lokaler macOS-ARM64-Companion in der Demo, ohne aktive Sprache: 13.440 KiB Prozess-RSS und 0,0 % CPU in einer `ps`-Momentaufnahme. **Browser nicht enthalten.** Der zweite Programmstart öffnet keinen zusätzlichen Dienst und beendet sich erfolgreich. Alle sechs Archiv-Prüfsummen und die lokale Ad-hoc-Signatur des macOS-Pakets wurden geprüft.

Reproduzierbar mit:

```sh
go test -race ./...
go vet ./...
npm ci
npm test
npm run check
npx playwright install chromium
npm run test:browser
go test -run '^$' -bench BenchmarkThousandRiders -benchmem ./internal/proximity
go run ./tests/load -clients 1000 -duration 90s
node scripts/release.mjs 1.0.0
```

Für einen kontrollierten gehosteten Test `ZWOICE_TEST_SERVER` und `ZWOICE_TEST_SIGNING_KEY` sicher als Prozessumgebung setzen und `npm run test:browser -- --grep 'two real'` ausführen. Der Schlüssel darf nicht in Shell-History, Repository, Testreports oder CI-Ausgaben geraten. Mit `ZWOICE_TEST_TURN_TLS=1` zusätzlich ausschließlich TURN/TLS erzwingen. Der Test nutzt die IDs 1001 und 1002; nicht parallel zu realen Nutzern mit diesen IDs ausführen.

## Noch erforderliche Freigabeprüfungen

1. **Echtes Zwift + Sauce:** Zwei echte Fahrer in derselben Welt nebeneinander, auseinanderfahren, aufschließen; Eventwechsel, Teleport, Kamerawechsel und Ende einer Fahrt prüfen. Tatsächliche Positionsauflösung und Portal-/Instanztrennung bestätigen.
2. **Audio und Netze:** Verständlichkeit mit Trainergeräusch und Headsets, unterschiedliche Internetanschlüsse/NATs, Paketverlust, WLAN-Wechsel, Mikrofon-/Ausgabegerätewechsel und Betrieb im Hintergrund prüfen.
3. **Mehrstündiger Dauerbetrieb:** Mindestens acht Stunden mit Sprechpausen, wechselnden Gruppen und TURN-Erneuerung über sechs Stunden; CPU, RSS, Verbindungszahl und Audiogeräte beobachten. Ein 90-Sekunden-Lastlauf ersetzt das nicht.
4. **Betriebssysteme:** Windows AMD64/ARM64, macOS Intel/Apple Silicon und Linux mit realen Audiogeräten ausführen. Browser-Energiesparen und Mikrofonberechtigungen prüfen. Cross-Compilation allein ist kein Laufzeittest.
5. **Distribution:** Für eine reibungsarme öffentliche Installation Apple Developer ID/Notarisierung und Windows-Code-Signing mit den Zertifikaten des Herausgebers einrichten. Diese Zugangsdaten liegen hier nicht vor.

Eine uneingeschränkte Aussage „vollständig produktionsgeprüft auf allen Plattformen“ ist bis zum Abschluss dieser Prüfungen nicht gerechtfertigt.
