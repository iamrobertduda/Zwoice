# Sicherheits- und Datenmodell

## Zugriff

Der normale Zugang ist **öffentlich und kontolos**. Der lokale Companion wartet auf eine frische eigene Sauce-Position und meldet deren Athleten-ID über einen HTTP-Header an. Alle normalen Nutzer landen in der Gruppe `public`. Eine Sitzung gilt höchstens 24 Stunden; nach Ende oder Neustart meldet der Client sich automatisch neu an. Pro Athleten-ID ist eine Sitzung gleichzeitig möglich; parallele Anmeldeversuche reservieren die ID schon vor dem TURN-Abruf.

Sauce liefert keine kryptografische Attestierung. Ein modifizierter Client kann daher **Identität und Position fälschen** und etwa eine fremde ID vorübergehend belegen. Automatische Erkennung ist keine Zwift-Kontoverifikation. Der offene Zugang erfüllt bewusst den kontolosen Start; Rate-Limits ersetzen keine Identitätsprüfung. Der Server vermittelt Signalisierung und ist Teil der Vertrauensgrenze.

Für bestehende eigene Integrationen versteht der Broker weiterhin signierte, zeitbegrenzte private Gruppen über den Authorization-Header. Die normale Oberfläche nutzt diesen Pfad nicht mehr; vorhandene Einladungstokens werden aus ihrem lokalen Speicher entfernt. Ein ungültiger Authorization-Header fällt nicht auf den öffentlichen Zugang zurück.

Nähe ist eine kooperative Anwendungsregel. Der Server vermittelt Signale nur entlang aktueller, aktiver Kanten. Die regulären Clients schließen Peer-Verbindungen, wenn diese Kante wegfällt. Zwei absichtlich modifizierte Clients könnten eine bereits aufgebaute direkte Verbindung weiterbetreiben. Proximity ist keine DRM- oder Geheimhaltungsgrenze gegen kooperierende Angreifer.

## Lokal

Die UI bindet nur an `127.0.0.1`. Hostname und Origin müssen exakt passen; API-Aufrufe ohne Origin werden verworfen. So kann eine beliebige fremde Website den lokalen Client nicht zum Auslesen von Sauce oder zum Öffnen einer Sitzung verwenden. Eine enge Content Security Policy verbietet fremde Skripte und Einbettung. Teilnehmernamen werden ausschließlich als Text ins DOM geschrieben.

Ein Prozess mit den Rechten des lokalen Benutzers bleibt außerhalb dieses Schutzmodells; er kann ebenso Browserdaten und Mikrofonberechtigungen beeinflussen.

## Übertragung

Entfernte Signalisierungsserver benötigen HTTPS/WSS. Nur für Loopback-Entwicklung ist HTTP zulässig. Die eigene Athleten-ID steht im `Zwoice-Athlete-ID`-Header des lokalen Go-Clients, nie in der URL. Optional unterstützte private Tokens bleiben im Authorization-Header. Der Browser sieht nur seine eigenen kurzlebigen ICE-Zugangsdaten.

WebRTC-Audio ist zwischen den Endpunkten mittels DTLS-SRTP verschlüsselt. TURN vermittelt diese verschlüsselten Pakete. Direkte P2P-Verbindungen können IP-Adressen gegenüber Gesprächspartnern offenlegen; die Einstellung „IP-Adresse verbergen“ erzwingt Relay-only und lehnt einen Server ohne TURN ab.

## Gespeicherte Daten

| Ort | Daten |
| --- | --- |
| Browser lokal | Einstellungen und stummgeschaltete IDs. Löschbar in Einstellungen. |
| Go-Client RAM | Aktuelle Sitzung, eigene Position und Verbindungskonfiguration. |
| Railway RAM | Aktuelle ID, Anzeigename, Gruppe, Position, Zeitstempel und Signalisierungsverbindungen. Bei Trennung gelöscht. Rate-Limit-Zähler speichern IP-Präfixe und Zeitstempel vorübergehend; inaktive Einträge werden beim nächsten Anmeldeversuch nach zehn Minuten entfernt, maximal 10.000 Einträge. |
| Cloudflare/coturn | Relay-Verbindungsmetadaten gemäß Anbieter-/Betreiberkonfiguration. |

Keine Audioaufzeichnung, Transkription, Analyse-Skripte oder Positionshistorie werden in Zwoice implementiert. Der Hostinganbieter kann technische Zugriffs-/Netzwerkdaten verarbeiten. Für einen öffentlichen Dienst muss der Betreiber seine konkrete Datenschutzinformation, Aufbewahrung und Anbietervereinbarungen passend zum tatsächlichen Betrieb festlegen.

## Missbrauch und Betrieb

Puffer, Nachrichtengröße und Gesamtverbindungen sind begrenzt. Öffentliche Verbindungen haben zusätzliche Grenzen:

- Höchstens acht gleichzeitig aktive/anmeldende Sitzungen je IPv4-Adresse oder IPv6-/64-Präfix.
- Pro IP-Präfix 20 Anmeldeversuche als Burst, danach ein neuer Versuch je zehn Sekunden.
- Global 60 Versuche als Burst, danach zwei je Sekunde; maximal 16 gleichzeitige TURN-Erstanfragen.
- Maximal 1.000 Clients und 10.000 gespeicherte IP-Limit-Einträge. Volle Limits antworten kontrolliert; Clients warten vor dem erneuten Versuch.
- TURN-Zugangsdaten gelten eine Stunde und werden nach 30 Minuten erneuert. Dauerhafte Cloudflare-Geheimnisse bleiben ausschließlich auf Railway.

IP-Header werden nur bei Ausführung auf Railway (`RAILWAY_PROJECT_ID`) übernommen, über den dort vom HTTP-Ingress gesetzten `X-Real-IP`-Header. [Railway-Spezifikation](https://docs.railway.com/networking/public-networking/specs-and-limits). Ohne diesen Modus zählt die direkte TCP-Gegenstelle; fremde Forwarding-Header werden ignoriert. Der Railway-Dienst darf nicht zusätzlich über einen ungeschützten TCP-Proxy erreichbar gemacht werden.

Die Limits begrenzen Anmelde- und Ressourcenmissbrauch; sie garantieren keine feste TURN-Bandreiten-/Kostengrenze. Gültige kurzlebige TURN-Zugangsdaten können von modifizierten Clients bis zum Ablauf anderweitig verwendet werden. Anbieter-Budgetalarme und Missbrauchsbeobachtung gehören zum öffentlichen Betrieb. Es gibt derzeit keine verifizierten Konten, persistente Sperrliste oder Meldungsstelle innerhalb der App. Stummschaltungen gelten lokal. Ein globaler Signierschlüsselwechsel betrifft nur den optionalen privaten Zugang.
