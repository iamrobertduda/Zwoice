# Zwoice starten

## Voraussetzungen

- Laufendes Zwift und Sauce for Zwift mit aktiviertem Webserver. Sauce muss den eigenen Zwift-Fahrer kennen.
- Ein aktueller Desktop-Browser mit WebRTC und Mikrofonzugriff. Chromium-basierte Browser sind durch automatisierte Ende-zu-Ende-Tests abgedeckt; andere Browser benötigen die unten beschriebene Geräteprüfung.
- Eine Zwoice-Serveradresse und eine persönliche, noch gültige Einladung vom Betreiber.
- Ein Mikrofon; ein Headset ist bei lautem Trainer praktisch.

## Betriebssysteme

**macOS:** Das passende ZIP entpacken (ARM64 für Apple Silicon, AMD64 für Intel), `Zwoice.app` in Programme verschieben und öffnen. Die Oberfläche startet im Standardbrowser. Ein nicht notarisiertes Paket kann von Gatekeeper blockiert werden; prüfe Quelle und Prüfsumme und verwende die gezielte Freigabe unter Systemeinstellungen → Datenschutz & Sicherheit. Schutzmechanismen nicht global deaktivieren.

**Windows:** ZIP entpacken und `Zwoice.exe` öffnen. Wähle AMD64 für übliche Intel-/AMD-Rechner, ARM64 für Windows-on-ARM. Ein nicht signiertes Paket kann eine SmartScreen-Prüfung auslösen. Die Anwendung braucht keine Administratorrechte.

**Linux:** Archiv entpacken, `zwoice` ausführbar starten (`./zwoice`). Für einen Menüeintrag kann eine `.desktop`-Datei mit dem absoluten Pfad zur Datei angelegt werden. Das Programm nutzt `xdg-open`, um den Standardbrowser zu öffnen. Es benötigt weder Node noch Desktop-Bibliotheken im Go-Prozess.

Die Oberfläche wird ausschließlich an `127.0.0.1` gebunden. Zwoice nicht als Administrator starten und den lokalen Port nicht öffentlich freigeben.

## Einmalige Einrichtung

1. Zwift und Sauce starten. Im Sauce-Webserver sollte `http://127.0.0.1:1080/api/athlete/v2/self?resource=state` deine eigene Zwift-ID und aktuelle `state`-Daten liefern.
2. In Zwoice **Einstellungen** öffnen. Anzeigename, Sauce-Adresse, Zwoice-Server und persönliche Einladung eintragen.
3. Optional „Einladung auf diesem Gerät merken“ aktivieren. Ohne diese Option gilt das Token nur für den geöffneten Tab.
4. **Fahrt starten** wählen und dem Browser den Mikrofonzugriff erlauben. Geräteauswahl steht nach dieser Freigabe mit Namen zur Verfügung.
5. Prüfen, dass sowohl Sauce als auch Zwoice verbunden sind. Stimmen werden automatisch hörbar, sobald andere aktive Teilnehmer in Reichweite sind.

Sauce darf auf einem anderen vertrauenswürdigen Gerät im lokalen Netz laufen. Dann dessen LAN-Adresse in Zwoice verwenden. Sauce nicht ungeschützt ins Internet stellen. Kein Sauce-Mod ist erforderlich.

## Während der Fahrt

- Mikrofon-Schaltfläche: eigene Übertragung stummschalten.
- „Ton pausieren“: sowohl Wiedergabe als auch eigene Übertragung pausieren.
- „Stumm“ an einer Person: nur diese Person lokal ausblenden. Die Auswahl bleibt auf diesem Gerät gespeichert.
- Lautstärke: Wiedergabe insgesamt regeln.
- Push-to-talk: Leertaste halten, wenn das Zwoice-Fenster den Fokus hat. Fokusverlust schaltet das Mikrofon wieder stumm. Es gibt keinen systemweiten Hotkey.
- Geräte oder Verbindungsdaten ändern: Einstellungen speichern und Fahrt neu starten. So wechselt die Audioverarbeitung vollständig und gibt alte Geräte frei.
- „Fahrt beenden“ stoppt Mikrofon und Verbindungen. „Zwoice beenden“ beendet zusätzlich das lokale Programm.

Der Tab darf minimiert werden, muss aber geöffnet bleiben. Browser-Energiesparfunktionen dürfen den Tab nicht schlafen legen. Andere Computer, Smartphones und Tablets können die lokale Loopback-Oberfläche nicht direkt aufrufen; mobile native Pakete werden nicht geliefert.

## Fehler beheben

| Anzeige | Maßnahme |
| --- | --- |
| Sauce nicht erreichbar | Sauce starten, Webserver/Port prüfen, LAN-Adresse korrigieren. |
| Keine frischen Positionsdaten | Eigene Fahrt in Zwift starten; `/self` in Sauce kontrollieren. Sprache bleibt bis zur Rückkehr aktueller Daten pausiert. |
| Einladung gehört zu anderer Zwift-ID | Neue Einladung für die von Sauce angezeigte eigene ID beziehen. |
| Einladung abgelaufen | Betreiber um eine neue Einladung bitten. |
| Fahrer bereits verbunden | Zweite Zwoice-Instanz oder anderen Tab schließen. |
| Verbunden, aber keine Sprachpartner | Beide müssen dieselbe Einladungsgruppe, Welt, Event-Untergruppe und Sportart haben; Mikrofonzugriff/Fahrtstart muss erfolgt sein. |
| Sprachverbindung gestört | TURN-Konfiguration und Netzwerk prüfen. Bei einem Wechsel des WLANs kurz auf die Wiederverbindung warten. |
| Mikrofon nicht verfügbar | System- und Browser-Berechtigung prüfen, Gerät anschließen und Systemstandard auswählen. |
| Ausgabe nicht wählbar | Browser unterstützt `setSinkId` nicht; Ausgabegerät im Betriebssystem wählen. |

## Daten löschen

„Gespeicherte Daten löschen“ entfernt Einstellungen, Einladungen und individuelle Stummschaltungen aus diesem Browser. Anschließend das Programm beenden und die entpackten Dateien löschen. Browser-Mikrofonberechtigungen bei Bedarf separat zurücksetzen.
