# Sauce for Zwift: verifizierte Integrationsgrundlage

Untersuchter Source-Stand: [`52adb9948d4ae88a4dcbd36db4a48b04987a9e45`](https://github.com/SauceLLC/sauce4zwift/tree/52adb9948d4ae88a4dcbd36db4a48b04987a9e45), abgerufen am 11. Oktober 2026. Sauce-Quellcode wurde zum Prüfen gelesen, nicht in Zwoice übernommen. Zusätzlich wurde das offizielle Paket **2.3.4** (09.10.2026) auf macOS ohne Zugangsdaten mit `--headless --help` gestartet und dessen Headless-/Storage-Schnittstelle geprüft. Der zentrale Stand ist noch nicht veröffentlicht; siehe [OBSERVERS.md](OBSERVERS.md).

## Schnittstellen

Sauce betreibt gewöhnlich einen HTTP-Webserver auf Port 1080 und einen optionalen HTTPS-Webserver auf 1081. Die API ist unter `/api` aufgelistet. Zwoice benutzt:

```text
GET /api/athlete/v2/{id}?resource=state&resource=athlete
WS  /api/ws/events
```

Nach dem Öffnen des WebSockets sendet Zwoice:

```json
{
  "type": "request",
  "uid": 1,
  "data": {
    "method": "subscribe",
    "arg": {
      "event": "athlete/{id}/v2",
      "subId": 2,
      "source": "stats",
      "options": { "resources": ["state", "athlete"], "stats": false }
    }
  }
}
```

Sauce bestätigt als `type: response`. Ereignisse verwenden `type: event`, `uid: 2`, `success: true` und die Fahrerressource in `data`. HTTP dient nur dem Start-Snapshot; danach läuft das Abonnement ohne dauerhaftes Positions-Polling. `{id}` wird durch die numerische ID aus der bestätigten Kontozuordnung ersetzt. Die Abonnements laufen ausschließlich im privaten Betreiber-Pool.

## Verwendete Felder

| Sauce-Feld | Bedeutung/Verarbeitung |
| --- | --- |
| `version` | Muss 2 sein. |
| `athlete.name`, `athlete.firstName`, `athlete.lastName` | Anzeigename automatisch übernehmen; leerer Name verhindert die Verbindung nicht. |
| `athleteId` | Muss zur autorisierten Fahrer-Lease passen. `self` ist für zentrale Beobachter nicht maßgeblich. |
| `age` | Alter im Sauce-Prozess in Millisekunden; Daten älter als 3 s werden nicht übernommen. |
| `state.worldTime` | Spiel-Zeitstempel; nur strikt neuere Werte aktualisieren eine Position. |
| `state.x/y/z` | Zentimeter in der virtuellen Welt, durch 100 in Meter umgerechnet. |
| `state.courseId` | Weltkennung. |
| `state.eventSubgroupId` | Trennung von Event-Untergruppen; fehlend bedeutet freie Fahrt (0). |
| `state.sport` | Sauce verwendet `cycling`/`running`; Zwoice normalisiert in interne Großschreibung. |

Sauce stellt im `PlayerState` keine Realm-ID bereit. Zwoice benutzt ausdrücklich Realm 1, das der untersuchte Sauce-Live-Client verwendet. Unbekannte/private Realms sind damit nicht abgedeckt.

Die Einheiten ergeben sich aus der Verarbeitung in `src/stats.mjs`: horizontale Koordinaten werden bei der geografischen Umrechnung durch `100` geteilt; Höhe wird aus `state.z / 100` bzw. mit Welt-Offset abgeleitet. Für geometrische Distanzen innerhalb derselben Welt bleiben die rohen relativen Koordinaten maßgeblich.

## Warum nicht Nearby oder Watching?

`nearby/v2` und `groups/v2` existieren, hängen aber am beobachteten Fahrer und können zahlreiche Nicht-Zwoice-Nutzer enthalten. Würden alle lokalen Nearby-Listen hochgeladen, entstünden redundante Daten und unnötige Datenschutz-/Bandbreitenkosten. Stattdessen verfolgt jeder zentrale Slot gezielt die numerische Fahrer-ID seiner autorisierten Sitzung. Nur diese Daten werden an den Broker weitergegeben. Ein Prozess je gleichzeitigem Fahrer ist zunächst die konservative Kapazitätsplanung; die reale Abdeckung muss gemessen werden.

`athlete/watching/v2` darf nicht als eigene Position verwendet werden: Ein Kamerawechsel würde sonst eine falsche Position und unerwünschte Gespräche erzeugen.

## Mod-Abwägung

Das [Sauce-Mod-Beispiel](https://github.com/SauceLLC/sauce4zwift-mod-example) unterstützt eigene Fenster mit einem `manifest.json`, `web_root` und optionalen Content-Skripten. Ein Mod ist für die benötigten Daten nicht erforderlich. Zwoice nutzt die externe API und bleibt unabhängig vom Electron-Fenster-/Mikrofonverhalten einer bestimmten Sauce-Version. Es wird kein zusätzlicher Mod installiert.

## Profile, Ausfälle und Messung

Die private RPC-API liefert Fahrersuche über `POST /api/rpc/v1/searchAthletes` mit `[query,{"limit":25,"pageLimit":1}]`. Eine Bestätigung verwendet `POST /api/rpc/v1/getAthlete` mit `[id,{"refresh":true,"allowFetch":true}]`. Der Broker akzeptiert keinen vom Nutzer eingesendeten Profiltext als Beweis. Profilaufrufe haben begrenzte Parallelität und eine gemeinsame Zeitgrenze über alle versuchten Prozesse.

Nach drei Sekunden ohne frische zentrale Position entfernt der Broker die Nähe. Dabei zählt Sauce-Datenalter plus Transportzeit. Wiederholte `worldTime`-Werte verlängern diese Frist auch nach einer Wiederverbindung nicht. Getrennte oder neu zugewiesene Leases können keine alte Position veröffentlichen. Prozessneustarts verwenden Backoff; bei Verlust der Steuerverbindung werden alle Beobachter beendet.

`pollMetrics` wird alle fünf Sekunden für Prozess-RSS und CPU abgefragt. Im geprüften Headless-Code ist `workingSetSize` in KiB; `percentCPUUsage` ist trotz seines Namens der Bruchteil eines CPU-Kerns. Zwoice rechnet in Bytes bzw. Prozent um. Fehlende Metriken werden als fehlende Messung ausgewiesen. Erreichbare API und frische Fahrerposition sind getrennte Zustände.

Das Netzwerkprotokoll ist aus tatsächlichem Quellcode abgeleitet. Ein echter Zwift-/Sauce-Test bleibt erforderlich, um Konto-Anmeldung, Profilverifikation, positionsbezogene Abdeckung, Aktualisierungsraten und Sonderwelten zu bestätigen. Der erfolgreiche Headless-Hilfsaufruf allein prüft diese Punkte nicht.

Quellen: [Webserver](https://github.com/SauceLLC/sauce4zwift/blob/52adb9948d4ae88a4dcbd36db4a48b04987a9e45/src/webserver.mjs), [Statistik-/Eventformat](https://github.com/SauceLLC/sauce4zwift/blob/52adb9948d4ae88a4dcbd36db4a48b04987a9e45/src/stats.mjs), [PlayerState/Sport-Enum](https://github.com/SauceLLC/sauce4zwift/blob/52adb9948d4ae88a4dcbd36db4a48b04987a9e45/src/zwift.proto), [Zwift-Decoder](https://github.com/SauceLLC/sauce4zwift/blob/52adb9948d4ae88a4dcbd36db4a48b04987a9e45/src/zwift.mjs).
