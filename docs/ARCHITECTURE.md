# Architekturentscheidungen

## Daten- und Audiopfad

```text
Zwift → privater Sauce-Pool → authentifizierter Worker → Railway
                                                          ↕
                      lokaler Go-Client ↔ Browseroberfläche
                                               ↕
                         WebRTC-Audio direkt oder über TURN
```

Dieser Entwicklungsstand verwendet Protokoll 2 und ist noch nicht veröffentlicht. [Betriebs- und Freigabestand](OBSERVERS.md). Nutzerpositionen kommen nur vom zentralen Worker; Nutzer wählen nicht selbst die Athleten-ID einer Sitzung.

## Go + vorhandener Browser

Ein kompiliertes Go-Programm enthält die Desktop-Oberfläche, Broker- und Worker-Modi. WebSockets verwenden `coder/websocket`, Konten `bbolt`. Keine React-/Electron-/Node-Runtime wird an Endnutzer ausgeliefert. Nur der Betreiber-Worker benötigt die separat installierte vollständige Sauce-Laufzeit und Node für die Konfiguration. Die standardisierte WebRTC-Implementierung des Browsers übernimmt Codecs, Jitterbuffer, Echounterdrückung, NAT-Traversal und Audiogeräte.

Das spart eine separat gebündelte Browser-Engine. Der vorhandene Browser verbraucht trotzdem RAM/CPU; seine Kosten dürfen bei Messungen nicht unterschlagen werden. Eine vollständig native Audio-Engine würde zusätzliche plattformspezifische Audio-, Echo- und Gerätewechsel-Implementierungen erfordern. Diese Version liefert eine lokale Anwendung mit Browseroberfläche, keinen nativen Tray-Client.

## Räumliche Auswahl

Die Position wird in Meter normiert. Raumtrennung erfolgt nach Gruppe (normalerweise `public`), Welt, Realm, Event-Untergruppe und Sport. Der Graph verwendet ein räumliches 3D-Raster mit 75-m-Zellen und betrachtet nur 27 Nachbarzellen. Die Paare werden nach Distanz sortiert; bestehende Paare bekommen eine 8-m-Präferenz gegen häufige Wechsel. Beide Endpunkte haben maximal sechs Kanten. Der Graph ist symmetrisch und bei gleichen Eingaben deterministisch.

Vier Aktualisierungen pro Sekunde sind ein Kompromiss aus Reaktion und Ressourcenverbrauch. Die Quelle selbst ist der begrenzende Faktor: Zwoice verspricht keine schnelleren Positionen als Sauce liefert. Die Lautstärke nähert sich auf dem Audiothread mit einer 120-ms-Zeitkonstante an den Zielwert an. Der weiche Verlauf ist `1 - smoothstep(10 m, 60 m, distance)`.

Geometrische Nähe ist keine Distanz entlang des Straßennetzes. Benachbarte Straßen können hörbar werden, sofern ihre 3D-Distanz klein genug ist. Nicht durch ein eigenes Realm-Feld unterscheidbare Spielinstanzen und besondere Portal-Geometrien müssen mit realen Sessions geprüft werden. Fehlende oder unbekannte Positionsdaten führen zu Stille, nicht zu einer geratenen Nähe.

## Begrenzte Last

- Maximal 1.000 angemeldete Clients pro Serverprozess (interne konfigurierbare Grenze).
- Maximal 64 aktive Fahrer pro räumlicher Zelle und Namespace. Weitere Fahrer erhalten eine klare Meldung und können nach Bewegung/frei werdender Kapazität wieder teilnehmen.
- Maximal sechs WebRTC-Verbindungen pro Fahrer.
- WebSocket-Nachrichten begrenzt; 32 ausgehende Nachrichten pro Client gepuffert. Ein zu langsamer Client wird getrennt.
- Begrenzte Nachrichtenrate und 16 gleichzeitig laufende TURN-Anmeldevorgänge.
- Keine globale Übertragung aller Fahrer und keine Audio-/Positionsaufzeichnung durch Zwoice. Konten und Fahrer-Verknüpfungen liegen dauerhaft in bbolt; Sauce hat eigene lokale Caches.

Dies ist eine bewusste Kapazitätsgrenze, keine Behauptung unbegrenzter Skalierung. Der 1.000-Fahrer-Benchmark prüft die Näheerkennung isoliert, nicht 1.000 reale Audioclients. Bevor die Betriebsgrenze erhöht wird, sind WebSocket-Lasttest, Egress-Messung und Profiler-Auswertung erforderlich.

## Skalierung des Betriebs

Eine Railway-Replik besitzt den Graphen. Das hält Betrieb und Kosten klein. Einfach mehr Repliken einzustellen wäre falsch: Bei zufälliger Verteilung könnten Nachbarn einander nicht finden. Für zusätzliche Kapazität sind explizite Shards nach Welt/Gruppe mit Routing der Teilnehmer und kontrollierter Übergabe beim Weltwechsel nötig. Ein gemeinsam erreichbarer Präsenzdienst könnte dafür ergänzt werden.

Für sehr große gleichzeitig hörbare Gruppen wäre ein SFU sinnvoll, der einen Audio-Uplink vervielfältigt. Ein Mesh mit begrenztem Grad ist für spontane kleine Gesprächsgruppen einfacher und spart dauerhaften Medienserver-Traffic. Die Grenze von sechs ist sichtbar; nicht jeder Fahrer in einem großen Feld hört jeden anderen. Das unterscheidet sich von einem klassischen unbegrenzten Sprachraum.

## Stabilität

Sitzungen sind an die Lebensdauer des Browser-Tabs gebunden. Alle Tracks, Peer-Verbindungen, Audioknoten und Timer werden beim Beenden freigegeben. Bei Netzwerkverlust wird zuerst der Audiopfad geschlossen und dann mit Backoff wieder verbunden. Mikrofonberechtigung wird nur auf Nutzeraktion angefordert. Ein ausgezogenes Mikrofon beendet die Sitzung mit einer Meldung, damit eine falsche Audioquelle nicht unbemerkt aktiviert wird.

Ein einzelner Offerer pro Teilnehmerpaar verhindert Offer-Kollisionen. ICE-Kandidaten werden bis zur Remote Description begrenzt zwischengespeichert. Pro Peer werden asynchrone Signale serialisiert. Neue Peer-Listen können veraltete Verbindungen sofort entfernen.

Cloudflare-/coturn-Zugangsdaten sind sitzungsbezogen und zeitlich begrenzt. Browser erhalten keine permanenten TURN- oder Signierschlüssel. Sprache nutzt DTLS-SRTP; der Signalisierungsserver hat keine Audioschlüssel.

## Einmalige Fahrer-Verknüpfung

Die App führt nach dem E-Mail-Code durch Fahrersuche und Bestätigung. Ein accountgebundener Zufallscode im Zwift-Profil beweist Zugriff auf diesen Fahrer. Die Bestätigung lädt das Profil frisch über einen Betreiber-Beobachter und verbraucht den 15 Minuten gültigen Code atomar. Pro Fahrer ist nur ein Zwoice-Konto erlaubt. Name und ID kommen anschließend aus dieser Zuordnung; Browsernachrichten können sie nicht überschreiben.

Nach „Fahrt starten“ reserviert der Broker einen Beobachterplatz. Erst frische, an diese Lease gebundene Positionsdaten aktivieren den Sprachgraphen. Der Worker verfolgt numerische Fahrer-IDs, keine Kameraposition und kein ungebundenes `self`. Veraltete Spiel-Zeitstempel und abgelaufene Leases können keine Nähe erneuern. Nach drei Sekunden Datenalter (Sauce-Alter plus Transportzeit) entfernt der Broker die Position. Der Client schaltet bei fehlender Quelle zusätzlich stumm.

Der Pool begrenzt seine Prozesse auf die konfigurierten Slots; der Broker standardmäßig auf 20. Ein separater Monitor-Login je gleichzeitigem Beobachter ist vorgesehen. Die tatsächliche Reichweite und Ressourcen pro Prozess sind erst mit realen Fahrern zu bestimmen. Ein Pool teilt einen Host; es gibt keine VM je Nutzer.

## Konten und Zugang

`internal/accounts` speichert Konten, gehashte Codes/Sitzungen, Zugangsregeln und widerrufbare Cloudflare-Grant-Zuordnungen in einer bbolt-Datei auf dem Railway-Volume. bbolt benötigt keinen separaten Datenbankprozess und keine CGO-Abhängigkeit. Ein Serverprozess besitzt die Datei exklusiv. Die Accounts-HTTP-API akzeptiert ausschließlich begrenzte POST-Anfragen; Rollen werden serverseitig aus der verifizierten Betreiber-E-Mail bestimmt. Der lokale Companion vermittelt diese API, hält das Login-Token aus JavaScript heraus und bindet sein HttpOnly-Cookie an den ausgewählten Server.

Die Ausgabe von TURN-Credentials prüft die Konto-Sitzung und reserviert atomar ein Tageskontingent vor dem externen Provideraufruf. Vor Weitergabe wird die Berechtigung erneut geprüft und der Credential-Besitz gespeichert. Ein gleichzeitig erfolgter Logout führt zum Widerruf; fehlgeschlagene Cloudflare-Widerrufe bleiben dauerhaft in der Warteschlange. Der Verbindungsgraph überprüft eingeloggte Konten alle zwei Sekunden.
