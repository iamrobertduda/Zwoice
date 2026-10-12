# Betrieb auf Railway

**Zentraler Entwicklungsstand:** Vor dem Deployment zusätzlich [OBSERVERS.md](OBSERVERS.md) beachten. Dieser Stand benötigt `ZWOICE_OBSERVER_TOKEN`, einen erreichbaren privaten Worker und bestätigte Fahrer-Verknüpfungen. Ohne Worker bleiben neue Sprachsitzungen geschlossen. Nicht über den bestehenden Dienst ausrollen, bevor reale Kapazität, Nutzungsgrundlage und passende Desktop-Pakete vorliegen.

## Aufteilung

Railway betreibt den Go-Signalisierungsdienst über HTTPS/WebSocket. Audio geht direkt zwischen den Teilnehmern oder über Cloudflare Realtime TURN. Railway braucht keine Audio-UDP-Ports und keinen separaten Datenbank- oder Redis-Dienst. Verifizierte Konten, Sitzungen und Zugangsregeln liegen in einer kleinen bbolt-Datei auf einem persistenten Volume (`/data`).

Die öffentliche Railway-Netzwerkschnittstelle unterstützt HTTP und TCP; eingehendes UDP ist nicht verfügbar. Ein gewöhnlicher UDP-SFU/TURN-Server wird deshalb nicht in denselben Railway-Container eingebaut. Quellen: [Railway TCP Proxy](https://docs.railway.com/networking/tcp-proxy), [Railway-Antwort zu UDP](https://station.railway.com/feedback/allow-outbound-udp-traffic-0f74101c).

## Railway einrichten

1. Neues Projekt **Zwoice** mit Service `zwoice` anlegen und dieses Repository als Service verbinden. Railway baut das `Dockerfile`.
2. Region möglichst nahe an der Hauptnutzergruppe wählen, zunächst Europa für eine europäische Community.
3. Ein persistentes Volume nach `/data` mounten und `ZWOICE_DATA_DIR=/data` setzen. Die App verweigert auf Railway den Start, wenn dieser Pfad nicht dem Volume entspricht. Der Container setzt die Verzeichnisrechte und läuft danach als UID 10001.
4. Cloudflare-Variablen sowie die E-Mail-Konfiguration aus den folgenden Abschnitten setzen.
5. Eine öffentliche Railway-Domain generieren. Die Basisadresse `https://…up.railway.app` wird im Client eingetragen.
6. Exakt **eine Replik** betreiben. Zwoice hält Präsenz im Prozessspeicher; unabhängig replizierte Instanzen würden Teilnehmer auf getrennte Graphen verteilen.
7. `/healthz` muss HTTP 200 liefern. Der Prüfpfad kontrolliert den Signalisierungsprozess, nicht die Erreichbarkeit des externen TURN-Dienstes.

Die aktuelle Infrastrukturdatei ist `.railway/railway.ts`. Nach `npm ci` und `railway link` zuerst `railway config plan` prüfen, dann `railway config apply` ausführen. Sie setzt Healthcheck, eine Replik, Docker-Build und zehn Sekunden Drain-Zeit. Cloudflare- und Login-Konfiguration werden mit `preserve()` ausdrücklich erhalten; Geheimnisse stehen nie in der Datei. Die Datei deklariert außerdem das 1-GiB-Kontenvolume. Der alte Signierschlüssel bleibt bei Migration erhalten, wird aber nicht mehr als Anmeldung akzeptiert. Bei weiteren manuell verwalteten Variablen diese ebenfalls mit `preserve()` aufnehmen: nicht deklarierte Variablen können vom IaC-Plan zur Löschung vorgesehen werden. Ein Plan mit unerwarteten Löschungen darf nicht angewendet werden.

Railway wertet `.railway/` nicht automatisch bei jedem Code-Deployment aus. Änderungen daran erfordern ein separates `config apply`. Das alte `railway.json` ist abgekündigt und wird deshalb nicht mehr ausgeliefert. [Railway IaC](https://docs.railway.com/infrastructure-as-code)

Der eingerichtete Dienst ist unter `https://zwoice-production.up.railway.app` erreichbar und in den Desktop-Paketen voreingestellt. Diese Adresse liefert Download-Seite und Signalisierung; die Bedienoberfläche läuft lokal im Desktop-Programm.

Railway nutzt standardmäßig „On Failure“ mit bis zu zehn Neustarts. [Neustartregeln](https://docs.railway.com/deployments/restart-policy)

`PORT` kommt von Railway. Als Startwert können `GOMEMLIMIT=128MiB` und eine passende RAM-Grenze gesetzt werden; nach Messung unter echter Last anpassen. Ein Containerneustart beendet laufende Verbindungen. Clients verbinden sich selbständig wieder und bauen Audio erneut auf.

## Empfohlen: Cloudflare Realtime TURN

1. Im Cloudflare-Dashboard unter Realtime einen TURN-Key anlegen.
2. Dessen Key-ID als `ZWOICE_CF_TURN_KEY_ID` und das zugehörige API-Token als `ZWOICE_CF_TURN_API_TOKEN` in Railway speichern.
3. Neu bereitstellen. Zwoice ruft den Cloudflare-Endpunkt zur Ausstellung kurzlebiger ICE-Zugangsdaten serverseitig auf. Die dauerhaften Geheimnisse verlassen den Server nicht.

Zugangsdaten gelten eine Stunde und werden bei laufenden Sitzungen nach 30 Minuten erneuert. Vorübergehende Fehler bei der Erneuerung werden nach einer Minute erneut versucht. Ein Fehler beim erstmaligen Abruf lässt den Sprachstart kontrolliert scheitern, statt eine vermeintlich zuverlässige Sitzung ohne Relay zu starten.

[Cloudflare-Anleitung](https://developers.cloudflare.com/realtime/turn/generate-credentials/)

Preisstand 11. Oktober 2026: Cloudflare nennt 1.000 GB Freivolumen für Realtime, darüber 0,05 USD pro GB. SFU und TURN teilen die Realtime-Abrechnung. Preise und Abrechnungsbedingungen vor Aktivierung im eigenen Konto kontrollieren. [Preise](https://developers.cloudflare.com/realtime/turn/faq/)

Audio-Bandbreite: Bei sechs dauerhaften Gegenstellen und 32 kbit/s sind es maximal ungefähr 192 kbit/s reine Audiobitrate pro Sender, zusätzlich RTP/UDP/DTLS-Overhead. DTX kann Sprechpausen reduzieren, die tatsächliche Unterstützung entscheidet der Browser. Die Relay-Quote, aktive Sprechzeit, Partnerzahl und Paket-Overheads bestimmen die Rechnung; eine pauschale Kosten-pro-Nutzer-Zusage wäre unzuverlässig.

## Alternative: eigener coturn

Auf einem Host mit öffentlicher UDP-Erreichbarkeit coturn mit `use-auth-secret`, `static-auth-secret`, restriktiven Relay-Regeln und TLS betreiben. Das gemeinsame Secret als `ZWOICE_TURN_SECRET` und die öffentlichen TURN-URLs als komma-separiertes `ZWOICE_TURN_URLS` setzen, etwa:

```text
turn:turn.example.com:3478?transport=udp,turns:turn.example.com:5349?transport=tcp
```

Cloudflare-Konfiguration hat Vorrang vor coturn. STUN kann mit `ZWOICE_STUN_URLS` überschrieben werden. Der Signalisierungsdienst erstellt zeitlich begrenzte coturn-REST-Zugangsdaten mittels HMAC-SHA1 gemäß dem TURN-REST-Verfahren.

## Selbstregistrierung mit kontrolliertem Zugang

Die Desktop-App fragt nach E-Mail und Einmalcode. Im zentralen Stand wird nach erfolgreicher Anmeldung der eigene Fahrer einmal bestätigt. Neue Sprachsitzungen benötigen einen freien Beobachterplatz; hörbar werden Teilnehmer erst mit frischen zentralen Daten. **Keine persönlichen Einladungen ausstellen.** Alte Clients ohne Login und alte Einladungstokens erhalten HTTP 401; Nutzer müssen v1.2.0 oder neuer installieren.

In Railway hinterlegen:

- `ZWOICE_RESEND_API_KEY`: API-Key für den Versand über Resend.
- `ZWOICE_MAIL_FROM`: z. B. `Zwoice <login@deine-domain.de>`, mit in Resend verifizierter Absenderdomain.
- `ZWOICE_ADMIN_EMAIL`: die E-Mail des Betreibers. Erst deren regulärer Login verleiht Zugriff auf **Dein Konto → Zugänge verwalten**.
- `ZWOICE_EMAIL_DAILY_LIMIT`: optionales Versandlimit, standardmäßig 90 pro UTC-Tag. Mit dem tatsächlichen Anbieter-Kontingent abstimmen.

Ohne vollständige Mailkonfiguration startet der Server, verweigert aber neue Anmeldungen. Es existieren kein Demo-Login, kein voreingestelltes Passwort und keine Freigabe über den alten Signierschlüssel. Erst erfolgreich zugestellte und bestätigte Codes erstellen Konten. Resend nennt derzeit 3.000 E-Mails pro Monat und 100 pro Tag im kostenlosen Tarif ([Preise](https://resend.com/pricing)); diese Grenze betrifft zusammen Registrierung und erneute Logins. Für größere Communities das Mailkontingent und das Zwoice-Tageslimit passend erhöhen.

Konten, Sperren und Sitzungshashes überleben Deployments. Cloudflare-Grants alter Verbindungen werden beim Neustart zum Widerruf vorgemerkt. Ein Railway-Volume bindet den Server an eine Replik; Wartung kann kurze Unterbrechungen auslösen. Railway hat beim Prüfen der Sicherungsfunktion gemeldet, dass manuelle Volume-Backups und Backup-Zeitpläne nur im Pro-Tarif verfügbar sind. Im aktuellen Hobby-Tarif sind daher **keine automatischen Backups aktiv**. Das persistente Volume schützt gegen den Verlust bei gewöhnlichen Deployments, ersetzt aber keine separate Sicherung. Bei einem späteren Pro-Wechsel kann ein täglicher Railway-Backup-Zeitplan aktiviert werden. Wiederherstellungen bei gestopptem Dienst vornehmen; eine Live-Kopie nur der bbolt-Datei ist kein garantiert konsistentes Backup.

Die öffentliche Startseite liefert Downloadlinks für v1.3.0. Pakete werden über GitHub Releases verteilt. Der gesamte Sprachzugang bleibt serverseitig geschützt, auch wenn jemand den Client verändert oder die UI umgeht. Grenzen und Vertrauensmodell stehen in [SECURITY.md](SECURITY.md).

## Freigabe

Vor öffentlicher Nutzung die Prüfungen in [VALIDATION.md](VALIDATION.md) abschließen: insbesondere echte Sauce-Sessions, Relay-only über unterschiedliche Internetanschlüsse, langfristige Sitzung und native OS-/Audiogeräteprüfungen. Erstellte Cross-Compilation-Pakete allein belegen keine Plattformfreigabe.
