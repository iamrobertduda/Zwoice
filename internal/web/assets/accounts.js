const $ = (id) => document.getElementById(id);
export class Accounts {
  current = null;
  challenge = "";
  next = "";
  constructor(server, changed, stop, notice) {
    Object.assign(this, { server, changed, stop, notice });
    $("accountButton").onclick = () => this.show();
    $("emailForm").onsubmit = (e) =>
      this.submit(e, async () => {
        const response = await this.call("request", {
          email: $("loginEmail").value.trim(),
        });
        this.challenge = response.challenge;
        $("emailForm").hidden = true;
        $("codeForm").hidden = false;
        $("codeDestination").textContent =
          `Wir haben deinen Code an ${$("loginEmail").value.trim()} angefordert. Er ist zehn Minuten gültig.`;
        $("loginCode").value = "";
        $("loginCode").focus();
      });
    $("codeForm").onsubmit = (e) =>
      this.submit(e, async () => {
        const response = await this.call("verify", {
          challenge: this.challenge,
          code: $("loginCode").value.replace(/\s/g, ""),
          remember: $("rememberLogin").checked,
        });
        this.set(response.account);
        $("accountDialog").close();
        this.notice("Du bist angemeldet. Jetzt kannst du deine Fahrt starten.");
      });
    $("changeEmail").onclick = () => {
      this.challenge = "";
      this.show();
    };
    $("logoutButton").onclick = () => this.logout();
    $("adminButton").onclick = async () => {
      $("accountDialog").close();
      $("adminDialog").showModal();
      await this.loadAdmin();
    };
    $("adminMore").onclick = () => this.loadAdmin(true);
    $("adminRefresh").onclick = () => this.loadAdmin();
    $("policyForm").onsubmit = (e) =>
      this.submit(
        e,
        async () => {
          await this.call("admin/policy", {
            registrationOpen: $("registrationOpen").checked,
            turnEnabled: $("turnEnabled").checked,
          });
          this.message("adminMessage", "Zugangsregeln gespeichert.");
        },
        "adminMessage",
      );
  }
  async call(action, payload = {}) {
    const response = await fetch(`/api/account/${action}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ server: this.server(), payload }),
      signal: AbortSignal.timeout(12000),
    });
    const result = response.status === 204 ? {} : await response.json();
    if (!response.ok) {
      const error = new Error(result.message || "Bitte versuche es erneut.");
      error.status = response.status;
      throw error;
    }
    return result;
  }
  set(account) {
    this.current = account;
    $("accountButton").textContent = account ? "Dein Konto" : "Anmelden";
    $("accountButton").title =
      account?.email || "Anmelden oder Konto erstellen";
    $("adminButton").hidden = !account?.admin;
    $("setupAccount").classList.toggle("done", Boolean(account));
    this.changed();
  }
  async refresh() {
    try {
      this.set((await this.call("me")).account);
    } catch (error) {
      if (error.status === 401 || error.status === 403) this.set(null);
    }
  }
  show() {
    $("emailForm").hidden = Boolean(this.current) || Boolean(this.challenge);
    $("codeForm").hidden = Boolean(this.current) || !this.challenge;
    $("accountDetails").hidden = !this.current;
    $("accountTitle").textContent = this.current
      ? "Dein Konto"
      : "Anmelden. Und losfahren.";
    $("accountEmail").textContent = this.current?.email || "";
    $("accountAccess").textContent = this.current?.turnAllowed
      ? "Sprachchat freigegeben"
      : "Sprachchat vom Betreiber gesperrt";
    this.message("loginMessage", "");
    $("accountDialog").showModal();
    if (!this.current) $(this.challenge ? "loginCode" : "loginEmail").focus();
  }
  ensure() {
    if (this.current) return true;
    this.show();
    return false;
  }
  async logout() {
    this.stop();
    $("logoutButton").disabled = true;
    try {
      await this.call("logout");
      this.notice("Du bist abgemeldet.");
    } catch {
      this.notice(
        "Lokal abgemeldet. Der Server war nicht erreichbar; die aktive Sprache wurde beendet.",
        true,
      );
    } finally {
      this.challenge = "";
      this.set(null);
      $("logoutButton").disabled = false;
      $("accountDialog").close();
    }
  }
  message(id, text, error = false) {
    $(id).textContent = text;
    $(id).hidden = !text;
    $(id).classList.toggle("error", error);
  }
  async submit(event, work, target = "loginMessage") {
    event.preventDefault();
    const button = event.submitter;
    if (button) button.disabled = true;
    this.message(target, "");
    try {
      await work();
    } catch (error) {
      this.message(target, error.message || "Bitte versuche es erneut.", true);
    } finally {
      if (button) button.disabled = false;
    }
  }
  async loadAdmin(append = false) {
    $("adminRefresh").disabled = true;
    $("adminMore").disabled = true;
    try {
      const result = await this.call("admin/accounts", {
        cursor: append ? this.next : "",
      });
      if (!append) $("accountList").replaceChildren();
      for (const account of result.accounts) this.accountRow(account);
      this.next = result.next;
      $("adminMore").hidden = !this.next;
      $("registrationOpen").checked = result.policy.registrationOpen;
      $("turnEnabled").checked = result.policy.turnEnabled;
      this.message("adminMessage", "");
    } catch (error) {
      this.message("adminMessage", error.message, true);
    } finally {
      $("adminRefresh").disabled = false;
      $("adminMore").disabled = false;
    }
  }
  accountRow(account) {
    const row = document.createElement("form");
    row.className = "account-row";
    const title = document.createElement("strong");
    title.textContent = account.email;
    const usage = document.createElement("p");
    usage.className = "field-note";
    usage.textContent = `${account.admin ? "Betreiber · " : ""}${account.turnDaily} TURN-Freigabeversuche heute`;
    const field = (text, type, value) => {
      const label = document.createElement("label");
      label.className = type === "checkbox" ? "check-label" : "";
      const input = document.createElement("input");
      input.type = type;
      if (type === "checkbox") input.checked = value;
      else {
        input.value = value;
        input.min = 1;
        input.max = 10000;
        input.required = true;
      }
      label.append(input, document.createTextNode(text));
      row.append(label);
      return input;
    };
    row.append(title, usage);
    const allowed = field(
      "Sprachchat / TURN erlauben",
      "checkbox",
      account.turnAllowed,
    );
    const blocked = field("Konto sperren", "checkbox", account.disabled);
    blocked.disabled = account.admin;
    const limit = field("TURN-Freigaben pro Tag", "number", account.turnLimit);
    const save = document.createElement("button");
    save.className = "secondary-button";
    save.textContent = "Speichern";
    save.type = "submit";
    row.append(save);
    row.onsubmit = (e) =>
      this.submit(
        e,
        async () => {
          await this.call("admin/access", {
            accountId: account.id,
            disabled: blocked.checked,
            turnAllowed: allowed.checked,
            turnLimit: Number(limit.value),
          });
          this.message(
            "adminMessage",
            `Zugang für ${account.email} aktualisiert.`,
          );
        },
        "adminMessage",
      );
    $("accountList").append(row);
  }
}
