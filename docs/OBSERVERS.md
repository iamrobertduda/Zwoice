# Zentrale Beobachter: Umsetzung und Freigabestand

Stand: 12. Oktober 2026. Dieser Entwicklungszweig enthält die zentrale Variante (Protokoll 2). Sie ist **nicht auf Railway umgestellt und nicht als Desktop-Release veröffentlicht**. Die öffentliche Website und ihre bestehenden Downloads beschreiben weiterhin die bisherige lokale Sauce-Version. Vor einer Umstellung müssen Server, Pakete und alle elf Website-Sprachen gemeinsam aktualisiert werden.

## Was Nutzer künftig tun

Zwoice installieren → mit E-Mail anmelden → den eigenen Zwift-Fahrer suchen und einmal bestätigen → Fahrt starten. Keine eigene Sauce-Installation, kein Beobachterkonto und keine VM beim Nutzer. Zur Bestätigung wird ein kurzlebiger Code vorübergehend an den Zwift-Namen angehängt; Zwoice liest ihn unabhängig über einen frischen Profilabruf. Danach darf er entfernt werden. Zwift-Passwörter werden von Endnutzern nicht abgefragt. Dieser Profilablauf wurde mit kontrollierten Profilantworten geprüft; die Übernahmezeit und Namensgrenzen in echtem Zwift müssen noch bestätigt werden.

## Öffentlich belegbare Bedingungen

Es wurde niemand kontaktiert. Die folgenden Aussagen sind Rechercheergebnisse; offene Punkte werden nicht als Erlaubnis ausgelegt.

| Quelle, geprüft am 12.10.2026 | Belegt | Nicht belegt |
| --- | --- | --- |
| [Sauce README](https://github.com/SauceLLC/sauce4zwift#headless-mode-advanced) | Betrieb ohne Fenster, Haupt- und Monitorzugang sowie ein gezielt beobachteter Fahrer per `--athlete-id`. | Gleichzeitige Abdeckung beliebig vieler verteilter Fahrer durch einen einzigen Login. |
| [Sauce-Produktseite](https://www.sauce.llc/products/sauce4zwift/) | Patreon-Mitgliedschaft, separates Monitor-Konto; nach Anbieterangabe braucht dieses keine aktive Zwift-Abrechnung. Derselbe Monitor-Login erlaubt keine parallelen Spielverbindungen. | Eine einzige Mitgliedschaft berechtigt ausdrücklich zum zentralen Dienst für alle Zwoice-Nutzer; konkrete Mehrnutzerpreise. |
| [Repository-Lizenz](https://github.com/SauceLLC/sauce4zwift/blob/52adb9948d4ae88a4dcbd36db4a48b04987a9e45/LICENSE), [Paketmetadaten](https://github.com/SauceLLC/sauce4zwift/blob/52adb9948d4ae88a4dcbd36db4a48b04987a9e45/package.json), [Sauce-Bedingungen](https://www.sauce.llc/legal/sauce4zwift-terms.html) | Im Repository liegt GPLv3; die Paketmetadaten nennen zugleich `UNLICENSED`. Die Dienstbedingungen regeln Nutzung allgemein. | Eindeutige Freigabe des gesamten ausgelieferten Pakets samt Assets/Abhängigkeiten als weiterverteilbarer Mehrnutzer-Hostingdienst. Die öffentliche API ist kein solcher Nachweis. |
| [Zwift-Bedingungen, 02.06.2026](https://eu.zwift.com/policies/terms-of-service) | §2b verlangt vorherige schriftliche Autorisierung für Konten im Namen einer Organisation. §5a enthält Einschränkungen für automatisierte Interaktionen; §5a xiii verlangt vorherige schriftliche Autorisierung für interagierende Anwendungen. | Dass eine etwaige Sauce-Autorisierung den separaten zentralen Zwoice-Dienst einschließt. |

**Ergebnis:** Headless-Betrieb ist technisch dokumentiert. Eine öffentliche, auf Zwoice übertragbare Freigabe für den geplanten zentralen Mehrnutzerbetrieb wurde nicht gefunden. Softwarelizenz, Sauce-Mitgliedschaft und Zwift-Zugangsbedingungen sind unterschiedliche Fragen. Der Repository-Lizenztext allein klärt sie nicht. Deshalb wird keine Freigabe und keine verbindliche Lizenzkostenkalkulation behauptet. Der Nutzer hat Kontaktaufnahme ausdrücklich ausgeschlossen; es wird keine Anfrage versendet und keine solche Freigabe eingeholt.

## Prozessmodell und Grenzen

Ein Worker verwaltet einen begrenzten Pool auf **einem gemeinsamen Linux-Host**, nicht eine VM je Nutzer. Im konservativen Modell benötigt jeder gleichzeitig verfolgte Fahrer einen Sauce-Prozess mit eigenem Monitor-Login. Ein Prozess bleibt bei leerem Pool für Fahrersuche und Profilbestätigung aktiv. Hauptzugänge gehören ebenfalls dem Betreiber. Ob derselbe Hauptzugang in allen Slots unter realen Bedingungen stabil und zulässig funktioniert, ist offen.

Zunächst gibt es genau einen Worker je Broker. Die Kapazität ist das Minimum aus konfigurierten Slots und `ZWOICE_OBSERVER_CAPACITY` (Standard 20). Das ist eine Obergrenze, **keine gemessene Zusage für 20 reale Fahrer**. Überzählige Anmeldungen werden kontrolliert abgewiesen. Kein automatisches Erstellen von Zwift-Konten, kein Umgehen von Sperren, Mitgliedschaft oder Sauce-Ausschlüssen.

Der Pool startet und beendet Prozesse entsprechend den aktiven Sitzungen. Wiederverwendung eines Slots wartet auf das Ende des vorherigen Prozesses. Verliert der Worker seine Steuerverbindung, beendet er alle Beobachter. Der Broker verwirft alte Positionen nach drei Sekunden; wiederholte Spiel-Zeitstempel verlängern die Frische nicht. Host und Railway benötigen synchronisierte Uhren.

## Private Bereitstellung vorbereiten

Voraussetzung: Nutzungsgrundlage geklärt, zulässige Betreiberkonten und eine Testumgebung vorhanden. Diese Voraussetzungen fehlen derzeit. Es wurde kein Server gemietet und kein kostenpflichtiger Dienst eingerichtet.

`deploy/observer/Dockerfile` baut ein **privates Betreiber-Image für Linux AMD64** mit dem unveränderten offiziellen Sauce-Paket 2.3.4. Version und SHA-256 sind festgeschrieben. Kein Sauce-Code landet in den Zwoice-Desktop-Paketen. Das öffentliche Quellrepository allein war hier keine vollständige Laufzeitgrundlage: benötigte Weltdaten fehlten, die optionale `zwift-utils`-Abhängigkeit war nicht öffentlich abrufbar.

Vorbereitetes Vorgehen auf dem Testhost:

1. `deploy/observer/observer.example.json` als `observer.json` im selben Ordner kopieren. Geheimnisse ausschließlich dort hinterlegen; die Datei ist von Git ausgeschlossen. Lesbar nur für Betreiber und Container-UID 1000 machen.
2. Für jeden gleichzeitigen Beobachter einen Slot mit separatem Monitor-Login ergänzen. Niemals Benutzerpasswörter oder einen gleichzeitig anderweitig verwendeten Monitorzugang eintragen.
3. Zufälliges gemeinsames Secret mit mindestens 32 Zeichen erzeugen. Im Worker unter `token`, auf dem **Testbroker** als `ZWOICE_OBSERVER_TOKEN` setzen. Serveradresse und getestete Kapazität anpassen.
4. Mit `docker compose -f deploy/observer/compose.yaml up -d --build` starten. Das Image nicht öffentlich veröffentlichen. Die dortigen 2 CPU / 4 GiB sind nur eine begrenzte Testkonfiguration; sie sind noch keine Größenempfehlung.
5. Betreiberansicht kontrollieren: belegte Plätze, frische Fahrer, erreichbare Prozesse und RAM der erreichbaren Sauce-Prozesse. „Online“ beschreibt die Steuerverbindung, nicht erfolgreiche Positionsversorgung.

Sauce bindet seine API an alle Interfaces im Container. **Keine Ports veröffentlichen, kein Host-Networking, keine öffentliche Sauce-URL.** Der Compose-Dienst veröffentlicht deshalb keinen Port. Nur der Worker verbindet sich ausgehend per WSS zum Broker. Die Containerdateien sind schreibgeschützt, mit separatem Datenvolume und ohne zusätzliche Linux-Capabilities. Auch andere nicht vertrauenswürdige Container gehören nicht in dieses Netz. Die private API erlaubt sensible RPCs und ist keine öffentliche Schnittstelle.

Das Datenvolume enthält Sauce-Einstellungen, Sitzungs-/Profildaten und mögliche lokale Caches. Es ist kein reiner Positionspuffer. Zugriff und Aufbewahrung entsprechend beschränken. Sauce-Ausgaben werden im Worker unterdrückt, damit entfernte Profile oder Zugangsdaten nicht in Sammellogs landen. Generische Neustartmeldungen und Messwerte bleiben verfügbar.

## Tatsächliche Kapazität messen

`tests/observer-live` startet einen privaten lokalen Broker und den echten Worker. Die produktive Serveradresse und das Secret in der eingelesenen Konfiguration werden für diesen Test ausschließlich im Speicher ersetzt. Keine Audio-Sitzungen, keine TURN-Ausgabe und keine synthetischen Positionen.

```sh
go run ./tests/observer-live \
  -config /private/path/observer.json \
  -riders 123456,234567 \
  -warmup 2m -duration 30m \
  -output observer-report.json
```

Die beiden Beispiel-IDs müssen durch die IDs zustimmender Testfahrer ersetzt werden. Zwei Fahrer sind ein Funktionstest, keine Kapazitätsfreigabe. Die Konfiguration muss zu tatsächlich installierten Laufzeitpfaden auf dem Testhost passen. Das bereits laufende Produktionspool darf nicht dieselben Monitorzugänge nutzen.

Nacheinander mit 10 und 20 echten, verteilten Fahrern testen. Mindestens zwei Welten und vier Gruppen mit mindestens 300 m Abstand oder getrennten Welt-/Eventkennungen; dieses Verteilungsprofil muss während mindestens 95 % der Messpunkte bestehen. Nach zwei Minuten Warmlauf mindestens 30 Minuten messen. Pro Fahrer werden Frischequote, längste Unterbrechung und p95 des Datenalters gespeichert. Mindestkriterien: 99 % frische Messpunkte je Fahrer und keine Lücke über drei Sekunden. Vollständige Prozessmesswerte müssen bei mindestens 95 % der Telemetrieproben vorliegen. Kurzläufe oder Abbrüche liefern keine erfolgreiche Qualifikation. Die erwartete Anzahl der Messpunkte wird aus der tatsächlichen Laufzeit berechnet; ausgelassene Messpunkte zählen gegen Frische- und Verteilungsquote. Mindestens 99 % der Positionsmesspunkte müssen vorliegen, und der Messprozess selbst darf keine Pause über eine Sekunde haben. Mehrfache Abfragen im selben Zeitintervall gleichen keine Lücken aus. Auch die Telemetriequote bezieht sich auf erwartete statt nur empfangene Intervalle. Das p95-Datenalter beschreibt ausschließlich die tatsächlich frischen Beobachtungen; fehlende Daten werden separat ausgewiesen.

Die JSON-Datei enthält echte Fahrer-IDs und wird privat mit Modus 0600 erzeugt; bestehende Dateien werden nicht überschrieben. CPU/RSS beziehen sich auf erreichbare Sauce-Prozesse, nicht auf das gesamte Betriebssystem, Worker oder Audioclients. Fehlende Telemetrie zählt ausdrücklich nicht als Nullverbrauch. Zusätzlich Host-RSS, CPU, Netzwerk und eventuelle Zwift-Ratenbegrenzung beobachten. Danach folgt ein achtstündiger Dauerlauf mit wechselnden Gruppen, Event-/Weltwechseln, Offline-Fahrern und einem Worker-Neustart. Gezielte Ausfallszenarien separat vom normalen Frischequotentest dokumentieren.

## Noch offen vor Umstellung

- Belegbare Nutzungsgrundlage für den zentralen Dienst; ohne Kontaktaufnahme derzeit öffentlich nicht abschließend nachweisbar.
- Betreiberkonten, Testhost und zustimmende echte Fahrer; reale 10-/20-Fahrer-Messung und Dauerlauf.
- Namensbestätigung mit echtem Zwift; ausgeschlossene Fahrer, Profil-Längenbegrenzung, Übernahmezeit, Events und Sonderinstanzen.
- Linux-Containerlauf mit authentifizierten Zwift-Sitzungen, reale Audiogeräte/Netze und weiterhin ausstehender produktiver Mailversand.
- Gemeinsames Release von Protokoll-2-Server und Desktop-Clients sowie angepasster Website. Bestehende Clients sind nicht mit dem neuen Sitzungsprotokoll kompatibel.

Die aktuelle Railway-Installation wurde deshalb nicht umgestellt. Lokale Testresultate stehen in [VALIDATION.md](VALIDATION.md).
