# Sauce for Zwift: verifizierte Integrationsgrundlage

Untersuchter Source-Stand: [`52adb9948d4ae88a4dcbd36db4a48b04987a9e45`](https://github.com/SauceLLC/sauce4zwift/tree/52adb9948d4ae88a4dcbd36db4a48b04987a9e45), abgerufen am 11. Oktober 2026. Sauce-Quellcode wurde zum Prüfen gelesen, nicht in Zwoice übernommen.

## Schnittstellen

Sauce betreibt gewöhnlich einen HTTP-Webserver auf Port 1080 und einen optionalen HTTPS-Webserver auf 1081. Die API ist unter `/api` aufgelistet. Zwoice benutzt:

```text
GET /api/athlete/v2/self?resource=state
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
      "event": "athlete/self/v2",
      "subId": 2,
      "source": "stats",
      "options": { "resources": ["state", "athlete"], "stats": false }
    }
  }
}
```

Sauce bestätigt als `type: response`. Ereignisse verwenden `type: event`, `uid: 2`, `success: true` und die Fahrerressource in `data`. HTTP dient nur dem Start-Snapshot; danach läuft das Abonnement ohne dauerhaftes Polling.

## Verwendete Felder

| Sauce-Feld | Bedeutung/Verarbeitung |
| --- | --- |
| `version` | Muss 2 sein. |
| `athlete.name`, `athlete.firstName`, `athlete.lastName` | Anzeigename automatisch übernehmen; leerer Name verhindert die Verbindung nicht. |
| `self`, `athleteId` | Es muss die eigene Fahrerressource sein; die ID dient automatisch zur Anmeldung. |
| `age` | Alter im Sauce-Prozess in Millisekunden; Daten älter als 3 s werden nicht übernommen. |
| `state.worldTime` | Spiel-Zeitstempel; nur strikt neuere Werte aktualisieren eine Position. |
| `state.x/y/z` | Zentimeter in der virtuellen Welt, durch 100 in Meter umgerechnet. |
| `state.courseId` | Weltkennung. |
| `state.eventSubgroupId` | Trennung von Event-Untergruppen; fehlend bedeutet freie Fahrt (0). |
| `state.sport` | Sauce verwendet `cycling`/`running`; Zwoice normalisiert in interne Großschreibung. |

Sauce stellt im `PlayerState` keine Realm-ID bereit. Zwoice benutzt ausdrücklich Realm 1, das der untersuchte Sauce-Live-Client verwendet. Unbekannte/private Realms sind damit nicht abgedeckt.

Die Einheiten ergeben sich aus der Verarbeitung in `src/stats.mjs`: horizontale Koordinaten werden bei der geografischen Umrechnung durch `100` geteilt; Höhe wird aus `state.z / 100` bzw. mit Welt-Offset abgeleitet. Für geometrische Distanzen innerhalb derselben Welt bleiben die rohen relativen Koordinaten maßgeblich.

## Warum nicht Nearby oder Watching?

`nearby/v2` und `groups/v2` existieren, hängen aber am beobachteten Fahrer und können zahlreiche Nicht-Zwoice-Nutzer enthalten. Würden alle lokalen Nearby-Listen hochgeladen, entstünden redundante Daten und unnötige Datenschutz-/Bandbreitenkosten. Stattdessen veröffentlicht jede laufende Zwoice-Instanz nur ihre eigene Position. Der Server findet unter den angemeldeten Zwoice-Fahrern die passenden Nachbarn.

`athlete/watching/v2` darf nicht als eigene Position verwendet werden: Ein Kamerawechsel würde sonst eine falsche Position und unerwünschte Gespräche erzeugen.

## Mod-Abwägung

Das [Sauce-Mod-Beispiel](https://github.com/SauceLLC/sauce4zwift-mod-example) unterstützt eigene Fenster mit einem `manifest.json`, `web_root` und optionalen Content-Skripten. Ein Mod ist für die benötigten Daten nicht erforderlich. Zwoice nutzt die externe API und bleibt unabhängig vom Electron-Fenster-/Mikrofonverhalten einer bestimmten Sauce-Version. Es wird kein zusätzlicher Mod installiert.

## Ausfälle

Der Client bündelt Updates auf höchstens vier pro Sekunde. Bei vier Sekunden ohne neues `worldTime` schaltet er den Sprachpfad stumm und entfernt die eigene aktive Position. Der Server entfernt unabhängig davon nach fünf Sekunden ohne frische Position alle Verbindungen des Fahrers. Wiederholte gecachte Pakete können diesen Ablauf nicht verhindern. WebSocket-Ausfälle führen zu Wiederverbindungen mit Backoff und Jitter.

Das Netzwerkprotokoll ist aus dem tatsächlichen Quellcode abgeleitet. Ein realer Zwift-/Sauce-Integrationstest bleibt erforderlich, um Laufzeitkonfiguration, Versionen und Sonderwelten zu bestätigen.

Quellen: [Webserver](https://github.com/SauceLLC/sauce4zwift/blob/52adb9948d4ae88a4dcbd36db4a48b04987a9e45/src/webserver.mjs), [Statistik-/Eventformat](https://github.com/SauceLLC/sauce4zwift/blob/52adb9948d4ae88a4dcbd36db4a48b04987a9e45/src/stats.mjs), [PlayerState/Sport-Enum](https://github.com/SauceLLC/sauce4zwift/blob/52adb9948d4ae88a4dcbd36db4a48b04987a9e45/src/zwift.proto), [Zwift-Decoder](https://github.com/SauceLLC/sauce4zwift/blob/52adb9948d4ae88a4dcbd36db4a48b04987a9e45/src/zwift.mjs).
