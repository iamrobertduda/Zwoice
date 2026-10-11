# Sicherheits- und Datenmodell

## Zugriff

Der Betreiber erstellt eine signierte Einladung mit Athleten-ID, Gruppe und Ablaufzeit. HMAC-SHA256 schützt diese Angaben gegen Änderungen. Vergleiche der Signatur erfolgen zeitkonstant. Abgelaufene Einladungen werden auch während laufender Verbindungen getrennt. Pro Athleten-ID kann nur eine Sitzung gleichzeitig aktiv sein.

Der lokale Client gleicht die eigene Sauce-ID mit der Einladung ab. Der Signalisierungsserver vertraut der übermittelten Position eines autorisierten Clients. Sauce liefert keine kryptografische Attestierung. Ein modifizierter Client kann deshalb seine Position fälschen. Einladungen sind eine kontrollierte Mitgliedschaft; ohne zusätzlichen Identitätsnachweis ist dies keine offene Registrierung mit verifiziertem Zwift-Kontobesitz. Der Signalisierungsdienst ist ebenfalls Teil der Vertrauensgrenze: Er vermittelt SDP und kann somit nicht als untrusted betrachtet werden.

Nähe ist eine kooperative Anwendungsregel. Der Server vermittelt Signale nur entlang aktueller, aktiver Kanten. Die regulären Clients schließen Peer-Verbindungen, wenn diese Kante wegfällt. Zwei absichtlich modifizierte Clients könnten eine bereits aufgebaute direkte Verbindung weiterbetreiben. Proximity ist keine DRM- oder Geheimhaltungsgrenze gegen kooperierende Angreifer.

## Lokal

Die UI bindet nur an `127.0.0.1`. Hostname und Origin müssen exakt passen; API-Aufrufe ohne Origin werden verworfen. So kann eine beliebige fremde Website den lokalen Client nicht zum Auslesen von Sauce oder zum Öffnen einer Sitzung verwenden. Eine enge Content Security Policy verbietet fremde Skripte und Einbettung. Teilnehmernamen werden ausschließlich als Text ins DOM geschrieben.

Ein Prozess mit den Rechten des lokalen Benutzers bleibt außerhalb dieses Schutzmodells; er kann ebenso Browserdaten und Mikrofonberechtigungen beeinflussen.

## Übertragung

Entfernte Signalisierungsserver benötigen HTTPS/WSS. Nur für Loopback-Entwicklung ist HTTP zulässig. Einladungen stehen im Authorization-Header des lokalen Go-Clients, nie in der URL. Der Browser sieht nur seine eigenen kurzlebigen ICE-Zugangsdaten.

WebRTC-Audio ist zwischen den Endpunkten mittels DTLS-SRTP verschlüsselt. TURN vermittelt diese verschlüsselten Pakete. Direkte P2P-Verbindungen können IP-Adressen gegenüber Gesprächspartnern offenlegen; die Einstellung „IP-Adresse verbergen“ erzwingt Relay-only und lehnt einen Server ohne TURN ab.

## Gespeicherte Daten

| Ort | Daten |
| --- | --- |
| Browser lokal | Einstellungen, auf Wunsch Einladung, stummgeschaltete IDs. Löschbar in Einstellungen. |
| Go-Client RAM | Aktuelle Sitzung, eigene Position und Verbindungskonfiguration. |
| Railway RAM | Aktuelle ID, Anzeigename, Gruppe, Position, Zeitstempel und Signalisierungsverbindungen. Bei Trennung gelöscht. |
| Cloudflare/coturn | Relay-Verbindungsmetadaten gemäß Anbieter-/Betreiberkonfiguration. |

Keine Audioaufzeichnung, Transkription, Analyse-Skripte oder Positionshistorie werden in Zwoice implementiert. Der Hostinganbieter kann technische Zugriffs-/Netzwerkdaten verarbeiten. Für einen öffentlichen Dienst muss der Betreiber seine konkrete Datenschutzinformation, Aufbewahrung und Anbietervereinbarungen passend zum tatsächlichen Betrieb festlegen.

## Missbrauch und Betrieb

Puffer, Nachrichtengröße, Rate und Gesamtverbindungen sind begrenzt. Der Dienst gibt kein vollständiges öffentliches Teilnehmerverzeichnis zurück. Einladungen können aktuell durch Ablauf oder durch Rotation des globalen Signierschlüssels ungültig gemacht werden; es gibt keine persistente individuelle Widerrufsliste. Für große öffentliche Communities sollten vor einer Öffnung Kontoverifikation, individuelle Sperren und Missbrauchsmeldungen ergänzt werden.
