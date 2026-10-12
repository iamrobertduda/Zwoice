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

Der Cloud-Test verwendet synthetische Mikrofon-Audiodaten und Sauce-Positionsdaten nach dem untersuchten echten API-Schema. Er verwendet **echtes WebRTC, den produktiven Railway-Dienst und Cloudflare TURN**, aber keine reale Zwift-Fahrt und keine zwei verschiedenen Internetanschlüsse. Die simulierten Athleten verwendeten in v1.1.0 den öffentlichen Zugang und eine zufällige synthetische Event-Untergruppe, um keine realen Fahrten zu erreichen. Der Test benötigt keine Signierschlüssel; Browser-Traces sind im gehosteten Test deaktiviert.

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
node scripts/release.mjs 1.2.0
```

Für einen kontrollierten Cloudflare-Test `ZWOICE_TEST_CF_KEY` und `ZWOICE_TEST_CF_TOKEN` ausschließlich als Prozessumgebung setzen und `npm run test:browser -- --grep 'two real'` ausführen. Der Broker läuft lokal mit echten Konto-Sitzungen und simuliertem Mailversand; Cloudflare transportiert reales WebRTC-Audio. Das ist kein Test des produktiven E-Mail-Versands oder des Railway-Loginpfads. Mit `ZWOICE_TEST_TURN_TLS=1` zusätzlich TURN/TLS erzwingen. Browser-Traces bleiben bei echten Credentials deaktiviert. `go test ./internal/broker -run TestCloudflareCredentialLifecycle -count=1` prüft unter denselben Variablen echte Ausgabe und Widerruf.

## Noch erforderliche Freigabeprüfungen

1. **Echtes Zwift + Sauce:** Zwei echte Fahrer in derselben Welt nebeneinander, auseinanderfahren, aufschließen; Eventwechsel, Teleport, Kamerawechsel und Ende einer Fahrt prüfen. Tatsächliche Positionsauflösung und Portal-/Instanztrennung bestätigen.
2. **Audio und Netze:** Verständlichkeit mit Trainergeräusch und Headsets, unterschiedliche Internetanschlüsse/NATs, Paketverlust, WLAN-Wechsel, Mikrofon-/Ausgabegerätewechsel und Betrieb im Hintergrund prüfen.
3. **Mehrstündiger Dauerbetrieb:** Mindestens acht Stunden mit Sprechpausen, wechselnden Gruppen und wiederholter TURN-Erneuerung; CPU, RSS, Verbindungszahl und Audiogeräte beobachten. Ein 90-Sekunden-Lastlauf ersetzt das nicht.
4. **Betriebssysteme:** Windows AMD64/ARM64, macOS Intel/Apple Silicon und Linux mit realen Audiogeräten ausführen. Browser-Energiesparen und Mikrofonberechtigungen prüfen. Cross-Compilation allein ist kein Laufzeittest.
5. **Distribution:** Für eine reibungsarme öffentliche Installation Apple Developer ID/Notarisierung und Windows-Code-Signing mit den Zertifikaten des Herausgebers einrichten. Diese Zugangsdaten liegen hier nicht vor.

Eine uneingeschränkte Aussage „vollständig produktionsgeprüft auf allen Plattformen“ ist bis zum Abschluss dieser Prüfungen nicht gerechtfertigt.

## Änderung v1.1.0: selbständiger Start

Lokale Go-Race-Tests prüfen zusätzlich öffentliche Anmeldung, ungültige IDs, doppelte und gleichzeitig anlaufende Sitzungen, IP-Raten-/Kapazitätslimits, Erholung nach Ablauf sowie die Vertrauensgrenze für Proxy-Header. Browserprüfungen decken einen leeren ersten Start ohne Einladung/Name/Einstellungen, das Warten über den Watchdog-Zeitraum hinweg, automatischen Fahrtbeginn, fehlendes Sauce und das Entfernen alter gespeicherter Einladungen ab. Die öffentliche Download-Seite bietet je einen Windows-Installer und eine universelle macOS-DMG; Linux behält zwei Architekturarchive.

Der [Prüflauf für v1.1.0 / d38b109](https://github.com/iamrobertduda/Zwoice/actions/runs/38122748233) ist mit allen sechs Jobs erfolgreich: Go-Race-Tests unter drei Betriebssystemen, fünf Browser-Ende-zu-Ende-Tests, sechs portable Pakete plus Mac-DMG sowie Windows-Installer einschließlich Installation, Healthcheck des installierten Programms und Deinstallation. Der erneute Cloud-Test mit öffentlichem Zugang bestätigt bidirektionales Opus über Cloudflare TURN/TLS (`relay/relay`, `relayProtocol=tls`) ohne Einladung oder Signierschlüssel. Die universelle Mac-DMG wurde schreibgeschützt eingebunden, ihre Signatur geprüft und die enthaltene App erfolgreich gestartet.

Windows-Installer: 5,82 MiB; universelle Mac-DMG: 6,45 MiB. Diese Größen beziehen sich auf v1.1.0. Die obenstehenden 1.000-Client- und Ressourcenmessungen stammen vom vorherigen Kernstand v1.0.0 und sind keine erneute Lastmessung des öffentlichen Anmeldepfades.


## Änderung v1.2.0: verifizierte Konten

Die Angaben zu anonymen Cloud-Tests weiter oben sind historisch (v1.1.0). v1.2.0 verweigert anonyme Zugänge und Einladungstokens. Lokal erfolgreich geprüft:

- Go-Race-Tests: dauerhafte Konten/Sitzungen, Code-Verbrauch und Ablauf, fünf Fehlversuche, E-Mail-Cooldown, Gerätebegrenzung, Adminberechtigung, Konto-/TURN-Sperren, Tageslimit, abgewiesene anonyme/alte Tokens ohne Provideraufruf, laufende Sprachverbindung nach Sperre beendet, Logout während TURN-Ausgabe, Wiederholung fehlgeschlagener Widerrufe und geschützte servergebundene Login-Cookies.
- Sechs Browserprüfungen: responsive Demo, zwei angemeldete WebRTC-Clients, Fahrt-Erkennung und Wiederverbindung, fehlendes Sauce, Downloads sowie Selbstregistrierung, HttpOnly-Cookie, persistenter Login, Abmelden und Betreiberregeln.
- Echte Cloudflare-Credential-Ausgabe und Revoke-API erfolgreich; zwei lokal angemeldete Clients übertragen bidirektionales Opus über echte Cloudflare-Relays (`relay/relay`). Das Postfach ist simuliert; es wurden keine echten Login-E-Mails versendet.
- 32 angemeldete Clients über 15 Sekunden: keine unerwarteten Trennungen, etwa 2,0 MiB kombinierter Go-Heap für Server und Lastgenerator, nach Aufräumen etwa 0,5 MiB. Dies ist ein kurzer Integrationslauf, keine Produktions-Lastgarantie.

Ausstehend: echter Mailversand von der verifizierten Betreiber-Domain, Login über diese Zustellung und die weiterhin oben genannten realen Zwift-/Sauce-/Geräte- und Dauerprüfungen. Go-Builds und Browser-Simulation allein ersetzen diese nicht.

Der Kontenserver wurde auf Railway erfolgreich bereitgestellt. Der Live-Healthcheck liefert HTTP 200; `/v1/session` auch mit Athleten-ID und WebSocket-Headern sowie `/v1/auth/me` liefern ohne Anmeldung HTTP 401. Ein persistentes 1-GiB-Volume ist unter `/data` angebunden. Die direkte Railway-API-Prüfung zeigte: Backup-Zeitpläne sind nur für Pro-Workspaces verfügbar, daher sind im aktuellen Hobby-Tarif keine automatischen Backups aktiv. Die erforderlichen Mailvariablen waren bei dieser Prüfung noch nicht vorhanden.

Der abschließende [Prüflauf 38124922236](https://github.com/iamrobertduda/Zwoice/actions/runs/38124922236) für Quellstand `1a61656` besteht alle sechs Jobs einschließlich Windows-Installation/Deinstallation, Go-Race-Tests auf Windows/macOS/Linux, sechs Browsertests und Paketbau. Alle neun veröffentlichten Assets von [v1.2.0](https://github.com/iamrobertduda/Zwoice/releases/tag/v1.2.0) wurden anhand ihrer GitHub-SHA-256-Digests mit den lokalen Dateien verglichen; die primären Downloads liefern HTTP 200.

## Änderung v1.3.0: Website und App im neuen Design

Die Landingpage orientiert sich an der am 12. Oktober 2026 geprüften [Discord-Website](https://discord.com/), die Desktop-Oberfläche an der [aktuellen Discord-Desktopdarstellung](https://support.discord.com/hc/en-us/articles/207260127-How-to-Change-Discord-Color-Themes-and-Customize-Appearance-Settings). Eigenes Zwoice-Logo, eigene Illustrationen und unveränderte Anmeldung/Sprachfunktionen. Keine Discord-Assets oder Laufzeitbibliotheken.

- Dunkle App, kompakte Navigation, Kontobereich, Mikrofon-, Teilnehmer-, Fehler- und Verbindungszustände sowie alle Dialoge überarbeitet.
- Responsive Website mit GitHub-Button und plattformspezifischen Downloads. Geprüft bei 320, 390, 768, 1024 und 1440 Pixeln Breite, ohne abgeschnittene Überschriften oder horizontales Scrollen.
- Zwei lokal eingebundene WOFF2-Schriften, insgesamt 46.772 Bytes; Lizenztexte in den Auslieferungshinweisen. Kein externer Font-Dienst.
- Lokal bestanden: Go Vet, alle Go-Race-Tests, zwei JavaScript-Tests, Syntaxprüfung und alle sechs Browser-Szenarien (einschließlich echter WebRTC-Audioverbindung, Mikrofonkontrolle, Entfernung, Wiederverbindung, verifizierter Anmeldung und Betreiberregeln).
- Screenshots von Website, Demo, Anmeldung, Einstellungen und Betreiberansicht visuell geprüft. Die Konten auf den Screenshots sind ausschließlich lokale Testkonten.

Die bestehende Mailkonfiguration auf Railway ist weiterhin Voraussetzung für echte Registrierungen. Dieses Design-Update verändert keine Zugangsregeln oder Infrastrukturvariablen.

### Landingpage-Überarbeitung

Die öffentliche Seite übernimmt weitere Muster der Discord-Website: schwebender Header beim Scrollen, Sternenhimmel mit Horizont im Hero, Laufband, farbige Funktionskacheln, drei Startschritte und ein abschließender Download-Aufruf. Der Abstands-Regler nutzt dieselbe Lautstärkekurve wie `distanceGain()` (voll bis 10 m, still ab 60 m). Animationen entfallen bei „Bewegung reduzieren“; ohne JavaScript bleibt alle Information sichtbar. Weiterhin nur lokale Schriften und Grafiken unter der bestehenden CSP, ohne Inline-Stile oder -Skripte.

Die Seite wird serverseitig aus `internal/web/site/download.html` und je einer Übersetzungsdatei gerendert: Deutsch, Englisch, Französisch, Niederländisch, Italienisch, Spanisch, Dänisch, Norwegisch, Schwedisch, Polnisch und Portugiesisch unter `/{sprache}/`, mit `hreflang`, Canonical-Link und Sprachwahl in Kopf- und Fußzeile. `/` leitet per `Accept-Language` weiter (302, `Vary: Accept-Language`), ohne passende Sprache auf Englisch; es wird kein Cookie gesetzt. Nicht-deutsche Seiten weisen darauf hin, dass die App selbst deutsch ist, und nennen die Schaltfläche „Fahrt starten“ mit Übersetzung.

- Alle elf Sprachen bei 320, 390, 768, 1024 und 1440 Pixeln Breite ohne horizontales Scrollen oder abgeschnittene Überschriften; keine Konsolen- oder CSP-Fehler. Per Skript erzeugte Texte (Download-Knopf, Abstands-Regler, Mikrofon-Vorschau) erscheinen in der Seitensprache, Zahlen und Einheiten über `Intl`.
- Go-Tests prüfen vollständige und gleich strukturierte Übersetzungen, das Rendern jeder Sprache, Weiterleitung, 404 für unbekannte Pfade und die Absicherung des Host-Headers für absolute Links.
- Lokal bestanden: `go vet`, alle Go-Race-Tests, JavaScript-Tests, `npm run check` und alle sechs Browser-Szenarien.
