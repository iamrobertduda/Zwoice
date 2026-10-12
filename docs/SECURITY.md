# Sicherheits- und Datenmodell

## Zugriff

Sprachchat und TURN benötigen ein **verifiziertes E-Mail-Konto und eine gültige Sitzung**. Die Selbstregistrierung erfordert keine Betreiber-Einladung: ein achtstelliger Einmalcode bestätigt die E-Mail. Codes sind zehn Minuten gültig, nach fünf Fehlversuchen gesperrt und nach erfolgreicher Verwendung verbraucht. Ungültige oder gesperrte Konten erhalten keine TURN-Zugangsdaten. Alte Einladungstokens und anonyme Athleten-Header werden ab v1.2 abgewiesen.

Sitzungen verwenden 256 Bit zufällige Tokens. Der Server speichert nur SHA-256-Hashes. Die lokale App legt den Token in ein HttpOnly-/SameSite-Strict-Cookie, gebunden an den ausgewählten Sprachserver; JavaScript und localStorage erhalten ihn nicht. Loopback-Cookies verwenden HTTP, entfernte Kommunikation ausschließlich HTTPS. Ohne „angemeldet bleiben“ gilt die Sitzung zwölf Stunden, andernfalls 30 Tage. Maximal fünf Geräteanmeldungen bleiben gültig. Eine sechste Anmeldung widerruft die älteste.

Die E-Mail aus `ZWOICE_ADMIN_EMAIL` erhält nach normaler Code-Verifikation Betreiberrechte. Weder der erste Nutzer noch vom Client gesendete Rollen werden privilegiert. Der Betreiber kann Konten sperren, TURN/Sprachchat pro Konto deaktivieren, Freigaben pro UTC-Tag begrenzen und Registrierung oder Sprachchat global abschalten. Nur eine Sprachverbindung je Konto und je Athleten-ID ist gleichzeitig möglich. Zugang und Status werden beim Verbinden, bei TURN-Erneuerung und alle zwei Sekunden während der Sitzung geprüft.

Im zentralen Entwicklungsstand wird zusätzlich der Zwift-Fahrer durch einen kurzlebigen Profilcode bestätigt. Der Broker liest das Profil unabhängig und frisch; Bestätigung und eindeutige Kontozuordnung erfolgen atomar. Das beweist Profilzugriff zum Prüfzeitpunkt, ist keine offizielle Zwift-Identitätsattestierung und erkennt spätere Kontoübernahmen nicht automatisch. Client-Positionen und Athleten-Header werden ignoriert. Positionsdaten müssen über den mit separatem Secret authentifizierten Worker kommen und eine gültige Fahrer-Lease sowie frische Spiel-Zeitstempel besitzen. Worker, Sauce, Profilquelle und Signalisierungsserver gehören zur Vertrauensgrenze. Ein kompromittierter Worker könnte Positionen oder Profilantworten fälschen.

Nähe ist eine kooperative Anwendungsregel. Der Server vermittelt Signale nur entlang aktueller, aktiver Kanten. Die regulären Clients schließen Peer-Verbindungen, wenn diese Kante wegfällt. Zwei absichtlich modifizierte Clients könnten eine bereits aufgebaute direkte Verbindung weiterbetreiben. Proximity ist keine DRM- oder Geheimhaltungsgrenze gegen kooperierende Angreifer.

## Lokal

Die UI bindet nur an `127.0.0.1`. Hostname und Origin müssen exakt passen; API-Aufrufe ohne Origin werden verworfen. So kann eine beliebige fremde Website den lokalen Client nicht zum Ändern der Fahrer-Verknüpfung oder zum Öffnen einer Sitzung verwenden. Eine enge Content Security Policy verbietet fremde Skripte und Einbettung. Teilnehmernamen werden ausschließlich als Text ins DOM geschrieben.

Ein Prozess mit den Rechten des lokalen Benutzers bleibt außerhalb dieses Schutzmodells; er kann ebenso Browserdaten und Mikrofonberechtigungen beeinflussen.

## Übertragung

Entfernte Signalisierungsserver benötigen HTTPS/WSS. Nur für Loopback-Entwicklung ist HTTP zulässig. Die eigene Athleten-ID wird serverseitig aus der bestätigten Kontozuordnung bestimmt; ein Athleten-Header hat keine Wirkung. Die Konto-Sitzung steht ausschließlich im Authorization-Header. Der Browser sieht nur seine eigenen kurzlebigen ICE-Zugangsdaten.

WebRTC-Audio ist zwischen den Endpunkten mittels DTLS-SRTP verschlüsselt. TURN vermittelt diese verschlüsselten Pakete. Direkte P2P-Verbindungen können IP-Adressen gegenüber Gesprächspartnern offenlegen; die Einstellung „IP-Adresse verbergen“ erzwingt Relay-only und lehnt einen Server ohne TURN ab.

## Gespeicherte Daten

| Ort | Daten |
| --- | --- |
| Browser lokal | Einstellungen, stummgeschaltete IDs und geschütztes Login-Cookie. Abmelden widerruft die Sitzung; „Daten löschen“ entfernt auch Einstellungen. |
| Railway-Volume | Verifizierte E-Mail, zufällige Konto-ID, Konto-/Loginzeit, Zugangsregeln, Tageszähler, bestätigte Zwift-ID/Name/Bestätigungszeit und vorübergehende Verknüpfungscodes; gehashte Sitzungen und Codes. Konto bleibt bis zur Betreiberlöschung gespeichert. Abgelaufene Codes/Sitzungen werden minütlich bereinigt. Mail-Ratelimits werden nach 24 Stunden bereinigt. TURN-Nutzerkennungen und Zuordnung zu Konto/Sitzung bleiben bis zum erfolgreichen Widerruf oder Ablauf. Keine Relay-Passwörter auf Disk. |
| Resend | Empfänger, Anmeldecode und Zustellungsmetadaten gemäß Anbieterbedingungen. |
| Go-Client RAM | Aktuelle Sitzung, Verbindungsstatus und Verbindungskonfiguration. |
| Privater Beobachter-Host | Betreiber-Zwift-Zugänge im geschützten Konfigurationsfile, getrennte Sauce-Profile/Caches auf dem Datenvolume; keine Nutzerpasswörter. Sauce kann weitere lokal verarbeitete Daten cachen. API-Ports sind ausschließlich intern erreichbar; Headless-Ausgaben werden nicht übernommen. |
| Railway RAM | Aktuelle ID, Anzeigename, Gruppe, Position, Zeitstempel und Signalisierungsverbindungen. Bei Trennung gelöscht. Rate-Limit-Zähler speichern IP-Präfixe und Zeitstempel vorübergehend; inaktive Einträge werden beim nächsten Anmeldeversuch nach zehn Minuten entfernt, maximal 10.000 Einträge. |
| Cloudflare/coturn | Relay-Verbindungsmetadaten gemäß Anbieter-/Betreiberkonfiguration. |

Keine Audioaufzeichnung, Transkription, Analyse-Skripte oder Positionshistorie werden in Zwoice implementiert. Der Hostinganbieter kann technische Zugriffs-/Netzwerkdaten verarbeiten. Für einen öffentlichen Dienst muss der Betreiber seine konkrete Datenschutzinformation, Aufbewahrung und Anbietervereinbarungen passend zum tatsächlichen Betrieb festlegen.

## Missbrauch und Betrieb

Puffer, Nachrichtengröße und Gesamtverbindungen sind begrenzt. Angemeldete Sprachverbindungen haben zusätzliche Grenzen:

- Höchstens acht gleichzeitig aktive/anmeldende Sitzungen je IPv4-Adresse oder IPv6-/64-Präfix.
- Pro IP-Präfix 20 Anmeldeversuche als Burst, danach ein neuer Versuch je zehn Sekunden.
- Global 60 Versuche als Burst, danach zwei je Sekunde; maximal 16 gleichzeitige TURN-Erstanfragen.
- Maximal 1.000 Clients und 10.000 gespeicherte IP-Limit-Einträge. Volle Limits antworten kontrolliert; Clients warten vor dem erneuten Versuch.
- TURN-Zugangsdaten gelten eine Stunde und werden nach 30 Minuten erneuert. Dauerhafte Cloudflare-Geheimnisse bleiben ausschließlich auf Railway.

IP-Header werden nur bei Ausführung auf Railway (`RAILWAY_PROJECT_ID`) übernommen, über den dort vom HTTP-Ingress gesetzten `X-Real-IP`-Header. [Railway-Spezifikation](https://docs.railway.com/networking/public-networking/specs-and-limits). Ohne diesen Modus zählt die direkte TCP-Gegenstelle; fremde Forwarding-Header werden ignoriert. Der Railway-Dienst darf nicht zusätzlich über einen ungeschützten TCP-Proxy erreichbar gemacht werden.

E-Mail-Codes sind zusätzlich pro E-Mail auf drei pro Stunde mit mindestens 60 Sekunden Abstand begrenzt. Standardmäßig werden höchstens 90 Login-E-Mails pro UTC-Tag versendet (`ZWOICE_EMAIL_DAILY_LIMIT`). Öffentliche Auth-Endpunkte haben begrenzte IP-/globale Budgets; Codes werden weder in Logs noch API-Antworten ausgegeben.

Cloudflare-Credentials werden vor der Ausgabe dauerhaft dem Konto, der Sitzung und Verbindung zugeordnet. Abmelden, Sperren, Wechsel/Entfernen der Fahrer-Verknüpfung, TURN-Abschaltung, Verbindungsende und Serverneustart markieren sie zum Widerruf. Ein Hintergrundprozess ruft die Cloudflare-Revoke-API auf; Fehler werden nach einer Minute erneut versucht, bis die einstündige Gültigkeit endet. Bei nicht erreichbarem Provider ist Widerruf verzögert. Alte vor dem v1.2-Upgrade ausgegebene Credentials sind nicht in dieser Datenbank erfasst und können noch bis zum Ablauf gelten. coturn hat hier keinen vergleichbaren Revoke-Pfad; dessen Credentials laufen aus.

Die Limits begrenzen Anmelde- und Ressourcenmissbrauch; sie garantieren keine feste TURN-Bandreiten-/Kostengrenze. Gültige kurzlebige TURN-Zugangsdaten können von modifizierten Clients bis zum Ablauf anderweitig verwendet werden. Anbieter-Budgetalarme und Missbrauchsbeobachtung gehören zum öffentlichen Betrieb. Tageslimits zählen Ausgabeversuche, auch bei Providerfehlern, und gelten nicht als Bandbreitenbudget. Stummschaltungen gelten lokal. Betreiber-Sperren wirken serverweit.
