# Mit Zwoice losfahren

**Diese Anleitung gilt für die veröffentlichten Pakete mit lokalem Sauce.** Der noch unveröffentlichte zentrale Entwicklungsstand führt nach der Anmeldung durch eine einmalige Fahrer-Verknüpfung und benötigt kein Sauce beim Nutzer. Details und noch offene Freigaben: [OBSERVERS.md](OBSERVERS.md).

[Zwoice herunterladen](https://zwoice-production.up.railway.app/), öffnen und **Anmelden & losfahren** anklicken. E-Mail eingeben und den achtstelligen Code aus dem Postfach bestätigen. Beim ersten Mal entsteht dein Konto automatisch. Anschließend **Fahrt starten** anklicken und das Mikrofon erlauben. Keine Einladung und keine manuelle Zwift-ID.

Mit „Auf diesem Gerät angemeldet bleiben“ gilt die Anmeldung 30 Tage. Ohne diese Option endet sie spätestens nach zwölf Stunden. Unter **Dein Konto → Abmelden** kannst du sie jederzeit beenden. Sprache und TURN funktionieren ausschließlich mit gültiger Anmeldung.

Zwift und **Sauce for Zwift** müssen laufen. Sauce braucht einen aktiven Webserver; dieser ist normalerweise bereits eingeschaltet. Zwoice zeigt direkt an, was noch fehlt, und wartet auf deine Fahrt. Sobald sie beginnt, geht es automatisch weiter. Die erstmalige Konto-/Beobachter-Einrichtung von Sauce bleibt in Sauce; Zwoice kann sie nicht übernehmen.

## Installieren

- **Windows:** `Zwoice-Windows-Setup.exe` öffnen. Der Installer wählt die passende Intel/AMD- oder ARM-Version, legt Startmenü- und Desktop-Verknüpfungen an und startet Zwoice auf Wunsch sofort. Keine Administratorrechte nötig. Deinstallation über Windows → Installierte Apps.
- **macOS:** `Zwoice-macOS.dmg` öffnen, Zwoice in „Applications/Programme“ ziehen und dort starten. Dasselbe Paket unterstützt Apple Silicon und Intel, ab macOS 12.
- **Linux:** Passendes Archiv entpacken und `zwoice` starten. AMD64 ist für Intel-/AMD-PCs, ARM64 für ARM-Geräte. Der Standardbrowser wird über `xdg-open` geöffnet.

Die aktuellen Windows-Pakete sind noch nicht mit einem Herausgeberzertifikat signiert; macOS ist lokal signiert, aber noch nicht notarisiert. Deshalb können SmartScreen oder Gatekeeper eine zusätzliche Freigabe verlangen. Für macOS die gezielte Freigabe in Systemeinstellungen → Datenschutz & Sicherheit verwenden; Schutzfunktionen nicht global deaktivieren.

Die Oberfläche öffnet sich im vorhandenen Desktop-Browser. Der Browser benötigt WebRTC und Mikrofonzugriff; Chromium ist automatisiert getestet. Zwoice läuft lokal auf `127.0.0.1`, nicht als Webseite auf dem Smartphone oder Apple TV. Zwift kann auf einem separaten unterstützten Gerät laufen, solange Sauce die eigene Fahrt liefert.

## Während der Fahrt

- **Mikrofon:** Eigene Stimme stummschalten.
- **Ton pausieren:** Wiedergabe und eigene Übertragung pausieren.
- **Stumm** bei einer Person: Nur diese Person ausblenden; bleibt lokal gespeichert.
- **Lautstärke:** Alle Stimmen gemeinsam regeln.
- **Fahrt beenden:** Mikrofon und Sprachverbindungen schließen.
- **Zwoice beenden:** Zusätzlich das lokale Programm beenden.

Lass den Browser-Tab geöffnet; Minimieren ist möglich. Der Tab darf nicht durch Browser-Energiesparen schlafen gelegt werden. Bei fehlenden Positionsdaten oder unterbrochener Verbindung pausiert die Sprache automatisch.

## Nur wenn du etwas ändern möchtest

Unter **Einstellungen** kannst du Audio-Geräte auswählen, einen abweichenden Anzeigenamen verwenden oder Push-to-talk einschalten. Push-to-talk funktioniert mit der Leertaste, solange das Fenster im Vordergrund ist. Nach Änderungen die Fahrt neu starten.

**Erweitert → Verbindung** enthält die Sauce- und Serveradressen. Die Standardwerte funktionieren auf demselben Computer. Läuft Sauce auf einem anderen vertrauenswürdigen Rechner im Heimnetz, trage dort dessen Sauce-Adresse ein. Keine Ports ins Internet freigeben. Ein Sauce-Mod ist nicht nötig.

## Wenn es hakt

| Anzeige | Nächster Schritt |
| --- | --- |
| Öffne Zwift und Sauce | Sauce starten. Falls es schon läuft: Sauce → Einstellungen → Allgemein/General → Web Server einschalten und Sauce neu starten. |
| Sauce ist da. Jetzt noch losfahren | Eigene Fahrt in Zwift beginnen; Sauce muss deinen Fahrer kennen. |
| Mikrofonzugriff nicht erlaubt | Mikrofon in den Browser-/Systemeinstellungen freigeben und erneut starten. |
| Fahrer bereits verbunden | Das andere Zwoice-Fenster schließen; die Verbindung wird automatisch erneut versucht. |
| Keine Sprachpartner | Andere müssen Zwoice aktiv nutzen und in derselben Welt, Event-Untergruppe und Sportart in deiner Nähe fahren. |

„Gespeicherte Daten löschen“ meldet dich ab und entfernt Einstellungen und persönliche Stummschaltungen. Dein Konto auf dem Server bleibt erhalten. Alte gespeicherte Einladungen werden beim Upgrade automatisch entfernt. Browser-Mikrofonberechtigungen verwaltet der Browser separat.
