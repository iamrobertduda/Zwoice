# Betrieb auf Railway

## Aufteilung

Railway betreibt den Go-Signalisierungsdienst über HTTPS/WebSocket. Audio geht direkt zwischen den Teilnehmern oder über Cloudflare Realtime TURN. Railway benötigt keine Audio-UDP-Ports, keine Datenbank, kein Redis und kein persistentes Volume.

Die öffentliche Railway-Netzwerkschnittstelle unterstützt HTTP und TCP; eingehendes UDP ist nicht verfügbar. Ein gewöhnlicher UDP-SFU/TURN-Server wird deshalb nicht in denselben Railway-Container eingebaut. Quellen: [Railway TCP Proxy](https://docs.railway.com/networking/tcp-proxy), [Railway-Antwort zu UDP](https://station.railway.com/feedback/allow-outbound-udp-traffic-0f74101c).

## Railway einrichten

1. Neues Projekt **Zwoice** mit Service `zwoice` anlegen und dieses Repository als Service verbinden. Railway baut das `Dockerfile`.
2. Region möglichst nahe an der Hauptnutzergruppe wählen, zunächst Europa für eine europäische Community.
3. Nur für ältere private API-Integrationen optional `ZWOICE_SIGNING_KEY` setzen (mindestens 32 zufällige Bytes). Der öffentliche Zugang braucht diesen Schlüssel nicht. Vorhandene Secrets werden vom IaC-Plan bewahrt.
4. Cloudflare-Variablen aus dem nächsten Abschnitt setzen.
5. Eine öffentliche Railway-Domain generieren. Die Basisadresse `https://…up.railway.app` wird im Client eingetragen.
6. Exakt **eine Replik** betreiben. Zwoice hält Präsenz im Prozessspeicher; unabhängig replizierte Instanzen würden Teilnehmer auf getrennte Graphen verteilen.
7. `/healthz` muss HTTP 200 liefern. Der Prüfpfad kontrolliert den Signalisierungsprozess, nicht die Erreichbarkeit des externen TURN-Dienstes.

Die aktuelle Infrastrukturdatei ist `.railway/railway.ts`. Nach `npm ci` und `railway link` zuerst `railway config plan` prüfen, dann `railway config apply` ausführen. Sie setzt Healthcheck, eine Replik, Docker-Build und zehn Sekunden Drain-Zeit. Die drei dauerhaften Secrets werden mit `preserve()` ausdrücklich erhalten; ihre Werte stehen nie in der Datei. Neue Betreiber müssen sie vor dem ersten Apply in Railway setzen. Bei weiteren manuell verwalteten Variablen diese ebenfalls mit `preserve()` aufnehmen: nicht deklarierte Variablen können vom IaC-Plan zur Löschung vorgesehen werden. Ein Plan mit unerwarteten Löschungen darf nicht angewendet werden.

Railway wertet `.railway/` nicht automatisch bei jedem Code-Deployment aus. Änderungen daran erfordern ein separates `config apply`. Das alte `railway.json` ist abgekündigt und wird deshalb nicht mehr ausgeliefert. [Railway IaC](https://docs.railway.com/infrastructure-as-code)

Der eingerichtete Dienst ist unter `https://zwoice-production.up.railway.app` erreichbar und in den Desktop-Paketen voreingestellt. Diese Adresse liefert Download-Seite und Signalisierung; die Bedienoberfläche läuft lokal im Desktop-Programm.

Railway nutzt standardmäßig „On Failure“ mit bis zu zehn Neustarts. [Neustartregeln](https://docs.railway.com/deployments/restart-policy)

`PORT` kommt von Railway. Als Startwert können `GOMEMLIMIT=128MiB` und eine passende RAM-Grenze gesetzt werden; nach Messung unter echter Last anpassen. Ein Containerneustart beendet laufende Verbindungen. Clients verbinden sich selbständig wieder und bauen Audio erneut auf.

## Empfohlen: Cloudflare Realtime TURN

1. Im Cloudflare-Dashboard unter Realtime einen TURN-Key anlegen.
2. Dessen Key-ID als `ZWOICE_CF_TURN_KEY_ID` und das zugehörige API-Token als `ZWOICE_CF_TURN_API_TOKEN` in Railway speichern.
3. Neu bereitstellen. Zwoice ruft den Cloudflare-Endpunkt zur Ausstellung kurzlebiger ICE-Zugangsdaten serverseitig auf. Die dauerhaften Geheimnisse verlassen den Server nicht.

Zugangsdaten gelten eine Stunde und werden bei laufenden Sitzungen nach 30 Minuten erneuert. Vorübergehende Fehler bei der Erneuerung werden nach einer Minute erneut versucht. Ein Fehler beim erstmaligen Abruf lässt die Anmeldung kontrolliert scheitern, statt eine vermeintlich zuverlässige Sitzung ohne Relay zu starten.

[Cloudflare-Anleitung](https://developers.cloudflare.com/realtime/turn/generate-credentials/)

Preisstand 11. Oktober 2026: Cloudflare nennt 1.000 GB Freivolumen für Realtime, darüber 0,05 USD pro GB. SFU und TURN teilen die Realtime-Abrechnung. Preise und Abrechnungsbedingungen vor Aktivierung im eigenen Konto kontrollieren. [Preise](https://developers.cloudflare.com/realtime/turn/faq/)

Audio-Bandbreite: Bei sechs dauerhaften Gegenstellen und 32 kbit/s sind es maximal ungefähr 192 kbit/s reine Audiobitrate pro Sender, zusätzlich RTP/UDP/DTLS-Overhead. DTX kann Sprechpausen reduzieren, die tatsächliche Unterstützung entscheidet der Browser. Die Relay-Quote, aktive Sprechzeit, Partnerzahl und Paket-Overheads bestimmen die Rechnung; eine pauschale Kosten-pro-Nutzer-Zusage wäre unzuverlässig.

## Alternative: eigener coturn

Auf einem Host mit öffentlicher UDP-Erreichbarkeit coturn mit `use-auth-secret`, `static-auth-secret`, restriktiven Relay-Regeln und TLS betreiben. Das gemeinsame Secret als `ZWOICE_TURN_SECRET` und die öffentlichen TURN-URLs als komma-separiertes `ZWOICE_TURN_URLS` setzen, etwa:

```text
turn:turn.example.com:3478?transport=udp,turns:turn.example.com:5349?transport=tcp
```

Cloudflare-Konfiguration hat Vorrang vor coturn. STUN kann mit `ZWOICE_STUN_URLS` überschrieben werden. Der Signalisierungsdienst erstellt zeitlich begrenzte coturn-REST-Zugangsdaten mittels HMAC-SHA1 gemäß dem TURN-REST-Verfahren.

## Öffentlicher Zugang

Die Desktop-App meldet Nutzer anhand ihrer frischen Sauce-`/self`-Position automatisch an. **Keine persönlichen Einladungen ausstellen.** `ZWOICE_SIGNING_KEY` ist für diesen Ablauf optional. Ein bestehender Schlüssel bleibt für ältere private Integrationen erhalten; er wird nicht in Desktop-Pakete eingebaut.

Die Startseite des Railway-Dienstes liefert die öffentliche Download-Seite. Pakete werden über GitHub Releases verteilt; das Go-Backend braucht dafür weder Download-Proxy noch Datenbank. Die Downloadlinks gehören zu v1.1.0 und müssen bei einer neuen Veröffentlichung gemeinsam mit den Release-Paketen aktualisiert werden.

Grenzen und Vertrauensmodell des kontolosen Zugangs stehen in [SECURITY.md](SECURITY.md). Bei einem Dienstneustart gehen laufende Sitzungen verloren und Clients verbinden sich erneut. Die globalen Anmeldelimits verteilen größere Wiederverbindungswellen zeitlich.

## Freigabe

Vor öffentlicher Nutzung die Prüfungen in [VALIDATION.md](VALIDATION.md) abschließen: insbesondere echte Sauce-Sessions, Relay-only über unterschiedliche Internetanschlüsse, langfristige Sitzung und native OS-/Audiogeräteprüfungen. Erstellte Cross-Compilation-Pakete allein belegen keine Plattformfreigabe.
