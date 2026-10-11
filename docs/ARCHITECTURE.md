# Architekturentscheidungen

## Daten- und Audiopfad

```text
Zwift → Sauce → lokaler Go-Client → Railway-Signalisierung
                      ↕                     ↕
                lokale Browser-UI ← Peer-Liste und SDP/ICE
                      ↕
          verschlüsseltes WebRTC-Audio (Opus)
                      ↕
           andere Browser, direkt oder via TURN
```

## Go + vorhandener Browser

Ein kompiliertes Programm enthält Server, Sauce-Adapter und 75 kB unkomprimierte UI-Quelldateien. Der Laufzeit-Code benötigt nur eine externe Go-Bibliothek (`coder/websocket`). Keine React-/Electron-/Node-Runtime wird ausgeliefert. Die standardisierte WebRTC-Implementierung des Browsers übernimmt Codecs, Jitterbuffer, Echounterdrückung, NAT-Traversal und Audiogeräte.

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
- Keine globale Übertragung aller Fahrer, keine Aufzeichnung und keine dauerhafte Datenbank.

Dies ist eine bewusste Kapazitätsgrenze, keine Behauptung unbegrenzter Skalierung. Der 1.000-Fahrer-Benchmark prüft die Näheerkennung isoliert, nicht 1.000 reale Audioclients. Bevor die Betriebsgrenze erhöht wird, sind WebSocket-Lasttest, Egress-Messung und Profiler-Auswertung erforderlich.

## Skalierung des Betriebs

Eine Railway-Replik besitzt den Graphen. Das hält Betrieb und Kosten klein. Einfach mehr Repliken einzustellen wäre falsch: Bei zufälliger Verteilung könnten Nachbarn einander nicht finden. Für zusätzliche Kapazität sind explizite Shards nach Welt/Gruppe mit Routing der Teilnehmer und kontrollierter Übergabe beim Weltwechsel nötig. Ein gemeinsam erreichbarer Präsenzdienst könnte dafür ergänzt werden.

Für sehr große gleichzeitig hörbare Gruppen wäre ein SFU sinnvoll, der einen Audio-Uplink vervielfältigt. Ein Mesh mit begrenztem Grad ist für spontane kleine Gesprächsgruppen einfacher und spart dauerhaften Medienserver-Traffic. Die Grenze von sechs ist sichtbar; nicht jeder Fahrer in einem großen Feld hört jeden anderen. Das unterscheidet sich von einem klassischen unbegrenzten Sprachraum.

## Stabilität

Sitzungen sind an die Lebensdauer des Browser-Tabs gebunden. Alle Tracks, Peer-Verbindungen, Audioknoten und Timer werden beim Beenden freigegeben. Bei Netzwerkverlust wird zuerst der Audiopfad geschlossen und dann mit Backoff wieder verbunden. Mikrofonberechtigung wird nur auf Nutzeraktion angefordert. Ein ausgezogenes Mikrofon beendet die Sitzung mit einer Meldung, damit eine falsche Audioquelle nicht unbemerkt aktiviert wird.

Ein einzelner Offerer pro Teilnehmerpaar verhindert Offer-Kollisionen. ICE-Kandidaten werden bis zur Remote Description begrenzt zwischengespeichert. Pro Peer werden asynchrone Signale serialisiert. Neue Peer-Listen können veraltete Verbindungen sofort entfernen.

Cloudflare-/coturn-Zugangsdaten sind sitzungsbezogen und zeitlich begrenzt. Browser erhalten keine permanenten TURN- oder Signierschlüssel. Sprache nutzt DTLS-SRTP; der Signalisierungsserver hat keine Audioschlüssel.

## Start ohne Einrichtung

Beim Öffnen prüft der lokale Client alle drei Sekunden die voreingestellte Sauce-Adresse, solange keine Sprachsitzung läuft und der Tab sichtbar ist. Dabei werden weder Mikrofon noch TURN-Zugangsdaten angefordert. Nach „Fahrt starten“ erlaubt der Nutzer sein Mikrofon; der Companion wartet auf eine frische eigene Fahrerposition, liest ID und Name aus `state` und `athlete`, und öffnet die öffentliche Sprachsitzung. Ein Fahrerwechsel beendet die alte Sitzung und verbindet automatisch unter der neuen ID. Bei fehlender Quelle bleibt das Mikrofon für die Übertragung gesperrt. Der Vorbereitungs-Heartbeat verhindert unnötige Wiederverbindungen während des Wartens.
