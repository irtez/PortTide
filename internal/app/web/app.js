"use strict";
const $ = (id) => document.getElementById(id),
  accessToken = document.querySelector('meta[name="porttide-token"]').content;
let state = null,
  inspected = "",
  shuttingDown = false,
  busy = false,
  lastSignature = "",
  toastTimer,
  refreshing = false;
const kindName = {
  latency: "HTTPS-задержка",
  geo: "IP и география",
  speed: "Скорость",
};
const date = (v) =>
  v && !v.startsWith("0001")
    ? new Date(v).toLocaleString()
    : "ещё не обновлялась";
const num = (v, d = 1) =>
  Number(v).toLocaleString(undefined, { maximumFractionDigits: d });
function text(tag, value, cls) {
  const e = document.createElement(tag);
  e.textContent = value;
  if (cls) e.className = cls;
  return e;
}
function show(id, on = true) {
  $(id).classList.toggle("hidden", !on);
}
function toast(message, error = false) {
  $("toast").textContent = message;
  $("toast").classList.toggle("error", error);
  show("toast");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => show("toast", false), error ? 7000 : 3000);
}
async function api(path, body, method) {
  const opts = {
    method: method || (body === undefined ? "GET" : "POST"),
    headers: { "X-PortTide-Token": accessToken },
  };
  if (opts.method !== "GET") {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body || {});
  }
  const response = await fetch("/api" + path, opts);
  let result;
  try {
    result = await response.json();
  } catch {
    throw new Error("Сервер не вернул ответ. Проверьте, запущен ли PortTide.");
  }
  if (!response.ok)
    throw new Error(result.error || "Не удалось выполнить действие");
  return result;
}
async function refresh() {
  if (shuttingDown || refreshing) return;
  refreshing = true;
  try {
    const next = await api("/state");
    const signature = JSON.stringify(next);
    state = next;
    if (signature !== lastSignature) {
      lastSignature = signature;
      render();
    }
  } catch (e) {
    $("notice").textContent =
      "Нет связи с PortTide. Запустите приложение и обновите страницу.";
    show("notice");
  } finally {
    refreshing = false;
  }
}
async function action(fn) {
  if (busy) return;
  busy = true;
  try {
    await fn();
    await refresh();
  } catch (e) {
    toast(e.message, true);
  } finally {
    busy = false;
  }
}
function findProfile(id) {
  return state?.profiles.find((p) => p.id === id);
}
function render() {
  $("version").textContent = "v" + state.version;
  $("profile-count").textContent = state.profiles.length;
  $("runtime-badge").textContent = state.running
    ? "Прокси запущен"
    : "Остановлен";
  $("runtime-badge").classList.toggle("online", state.running);
  const active = findProfile(state.running ? state.active : state.selected);
  $("active-name").textContent =
    state.running && active
      ? active.name
      : active
        ? "Готов к запуску: " + active.name
        : state.profiles.length
          ? "Выберите профиль для подключения"
          : "Выберите свой первый профиль";
  $("active-hint").textContent = state.running
    ? "Приложения могут использовать этот вход. Доступность сети проверяется отдельно."
    : "Профили можно проверять до подключения. Системная маршрутизация остаётся под вашим управлением.";
  show("stop-proxy", state.running);
  const p = state.proxy;
  $("proxy-address").textContent =
    p.mode === "mixed"
      ? "HTTP / SOCKS5 · 127.0.0.1:" + p.socksPort
      : "SOCKS5 :" + p.socksPort + " · HTTP :" + p.httpPort;
  $("auth-label").textContent = p.auth ? "С авторизацией" : "Без авторизации";
  const notice =
    state.notice ||
    (!state.coreAvailable
      ? "Ядро Xray не найдено. Используйте полную сборку или укажите путь в настройках."
      : "");
  $("notice").textContent = notice;
  show("notice", Boolean(notice));
  renderSources();
  renderRows();
  renderDiagnostics();
}
function renderSources() {
  const list = $("source-list"),
    filter = $("source-filter"),
    selected = filter.value;
  list.replaceChildren();
  filter.replaceChildren(new Option("Все подписки", ""));
  for (const s of state.sources) {
    filter.add(new Option(s.name, s.id));
    const item = text("div", "", "source-item"),
      title = text("button", s.name, "source-item-title text-button");
    title.onclick = () => metadata(s);
    item.append(
      title,
      text(
        "div",
        s.error
          ? "Ошибка обновления · кеш сохранён"
          : s.count + " записей · " + date(s.updatedAt),
        "source-item-summary",
      ),
    );
    const controls = text("div", "", "source-buttons");
    for (const [label, fn] of [
      [
        "Обновить",
        () =>
          action(async () => {
            await api("/sources/" + s.id + "/refresh", {});
            toast("Подписка обновлена");
          }),
      ],
      ["Изменить", () => sourceDialog(s)],
      [
        "Удалить",
        () => {
          if (confirm("Удалить подписку «" + s.name + "»?"))
            action(() => api("/sources/" + s.id, {}, "DELETE"));
        },
      ],
    ]) {
      const b = text("button", label, "text-button");
      b.onclick = fn;
      if (label === "Обновить" && !s.remote) b.disabled = true;
      controls.append(b);
    }
    item.append(controls);
    list.append(item);
  }
  if (!state.sources.length)
    list.append(text("p", "Источники ещё не добавлены", "muted small"));
  filter.value = selected;
}
function renderRows() {
  if (!state) return;
  const query = $("search").value.toLocaleLowerCase(),
    source = $("source-filter").value;
  const profiles = state.profiles.filter(
    (p) =>
      (!source || p.sourceId === source) &&
      p.name.toLocaleLowerCase().includes(query),
  );
  $("filtered-count").textContent =
    profiles.length + " / " + state.profiles.length;
  show("empty", !state.profiles.length);
  show("profile-table-wrap", Boolean(profiles.length));
  show("no-matches", state.profiles.length > 0 && !profiles.length);
  const tbody = $("profiles");
  tbody.replaceChildren();
  for (const p of profiles) {
    const row = $("profile-template").content.firstElementChild.cloneNode(true);
    row.dataset.id = p.id;
    row.classList.toggle("active", state.running && state.active === p.id);
    row.classList.toggle("selected", inspected === p.id);
    row.querySelector(".profile-name").textContent = p.name;
    row.querySelector(".profile-meta").textContent =
      p.protocols.join(" · ") +
      (state.running && state.active === p.id ? " · АКТИВЕН" : "");
    row.querySelector(".profile-reason").textContent = p.supported
      ? ""
      : p.reason;
    const select = row.querySelector('[data-action="select"]');
    select.disabled =
      !p.supported ||
      !state.coreAvailable ||
      (state.running && state.active === p.id);
    if (state.running && state.active === p.id)
      select.textContent = "Подключён";
    const latency = state.results[p.id + ":latency"]?.measurements.find(
      (m) => m.name === "Median",
    );
    if (latency)
      row.querySelector(".latency-cell").textContent =
        num(latency.value, 0) + " мс";
    const geo =
      state.results[p.id + ":geo"]?.measurements.filter(
        (m) => m.kind === "observer" && !m.error,
      ) || [];
    if (geo.length)
      row.querySelector(".geo-cell").textContent = [
        ...new Set(geo.map((m) => m.country || m.ip)),
      ].join(" / ");
    const speed = state.results[p.id + ":speed"]?.measurements.find(
      (m) => m.kind === "download" && !m.error,
    );
    if (speed)
      row.querySelector(".speed-cell").textContent =
        num(speed.value) + " Мбит/с";
    tbody.append(row);
  }
}
function inspect(id) {
  inspected = id;
  renderRows();
  renderDiagnostics();
  $("diagnostics-panel").scrollIntoView({
    behavior: "smooth",
    block: "nearest",
  });
}

function renderDiagnostics() {
  if (!state) return;
  const p = findProfile(inspected);
  $("diagnostic-title").textContent = p
    ? p.name
    : "Проверьте профиль перед выбором";
  show("test-actions", Boolean(p));
  show("cancel-test", state.job.running);
  show("test-progress", state.job.running);
  const tested = findProfile(state.job.profileId);
  $("progress-text").textContent =
    (kindName[state.job.kind] || "Проверка") + " · " + (tested?.name || "");
  for (const b of document.querySelectorAll("[data-test]"))
    b.disabled = !p?.supported || !state.coreAvailable || state.job.running;
  const results = $("test-results");
  results.replaceChildren();
  let count = 0;
  for (const kind of ["latency", "speed", "geo"]) {
    const result = state.results[inspected + ":" + kind];
    if (!result) continue;
    count++;
    const block = text("div", "", "result-block"),
      head = text("div", "", "result-heading");
    head.append(
      text("h3", kindName[kind]),
      text("span", date(result.at), "muted small"),
    );
    block.append(head);
    if (result.error) block.append(text("div", result.error, "result-error"));
    if (kind === "geo") {
      block.append(
        text(
          "p",
          "Наблюдатели показывают видимый им выход. Базы сравниваются для явно указанного IP; их ответы могут различаться.",
          "geo-note",
        ),
      );
      const wrap = text("div", "", "table-scroll"),
        table = text("table", "", "geo-table"),
        thead = document.createElement("thead"),
        hr = document.createElement("tr");
      for (const h of [
        "Тип / источник",
        "IP",
        "Страна / город",
        "ASN / организация",
      ])
        hr.append(text("th", h));
      thead.append(hr);
      table.append(thead);
      const body = document.createElement("tbody");
      for (const m of result.measurements) {
        const row = document.createElement("tr");
        row.append(
          text(
            "td",
            (m.kind === "observer" ? "Наблюдатель · " : "База · ") + m.name,
          ),
          text("td", m.ip || "—"),
          text(
            "td",
            m.error || [m.country, m.region].filter(Boolean).join(" · ") || "—",
            m.error ? "result-error" : "",
          ),
          text(
            "td",
            [m.asn, m.organization].filter(Boolean).join(" · ") || "—",
          ),
        );
        body.append(row);
      }
      table.append(body);
      wrap.append(table);
      block.append(wrap);
    } else {
      const grid = text("div", "", "result-grid");
      for (const m of result.measurements) {
        const card = text("div", "", "metric");
        const label =
          m.kind === "download"
            ? "Скачивание ↓"
            : m.kind === "upload"
              ? "Отдача ↑"
              : m.name === "Median"
                ? "Медиана"
                : m.name.replace("HTTPS sample ", "Замер ");
        card.append(text("small", label));
        if (m.error) card.append(text("p", m.error, "result-error"));
        else {
          card.append(
            text(
              "div",
              num(m.value, m.kind === "latency" ? 0 : 1) +
                (m.kind === "latency" ? " мс" : " Мбит/с"),
              "metric-value",
            ),
          );
          if (m.bytes)
            card.append(
              text(
                "small",
                num(m.bytes / 1e6) + " MB · " + num(m.seconds) + " с",
              ),
            );
        }
        grid.append(card);
      }
      block.append(grid);
    }
    results.append(block);
  }
  show("diagnostic-empty", !count);
}
function sourceDialog(s) {
  $("source-form").reset();
  $("source-id").value = s?.id || "";
  $("source-name").value = s?.name || "";
  $("source-agent").value = s?.userAgent || "Happ/2.0.0";
  $("source-dialog-title").textContent = s
    ? "Изменить подписку"
    : "Новая подписка";
  $("source-url-hint").textContent = s?.remote
    ? "Текущий источник: " +
      s.urlHint +
      ". Оставьте ссылку пустой, чтобы сохранить её."
    : "Незашифрованный Xray JSON. Ссылка и ключи хранятся только локально.";
  $("source-error").textContent = "";
  $("source-dialog").showModal();
}
function settingsDialog() {
  if (!state) return;
  const p = state.proxy;
  $("proxy-mode").value = p.mode;
  $("socks-port").value = p.socksPort;
  $("http-port").value = p.httpPort;
  $("proxy-auth").checked = p.auth;
  $("proxy-user").value = p.username;
  $("proxy-password").value = p.password;
  $("proxy-password").type = "password";
  $("show-password").checked = false;
  $("proxy-udp").checked = p.udp;
  $("core-path").value = state.corePath;
  $("core-path").placeholder = state.coreResolved;
  $("refresh-minutes").value = state.refreshMinutes;
  $("settings-error").textContent = "";
  updateSettingsControls();
  $("settings-dialog").showModal();
}
function updateSettingsControls() {
  $("http-port").disabled = $("proxy-mode").value === "mixed";
  $("proxy-user").disabled = $("proxy-password").disabled =
    !$("proxy-auth").checked;
}
function metadata(s) {
  $("metadata-title").textContent = s.name;
  const node = $("metadata-content");
  node.replaceChildren();
  const entries = {
    Источник: s.urlHint,
    Обновление: date(s.updatedAt),
    ...(s.error ? { Ошибка: s.error } : {}),
    ...friendlyMetadata(s.metadata || {}),
  };
  for (const [key, value] of Object.entries(entries)) {
    const row = text("div", "", "metadata-row");
    row.append(text("strong", key), text("span", value));
    node.append(row);
  }
  $("metadata-dialog").showModal();
}
for (const id of ["add-source", "add-source-small", "empty-add"])
  $(id).onclick = () => sourceDialog();
for (const id of ["open-settings", "connection-details"])
  $(id).onclick = settingsDialog;
for (const b of document.querySelectorAll("[data-close]"))
  b.onclick = () => $(b.dataset.close).close();
$("search").oninput = renderRows;
$("source-filter").onchange = renderRows;
$("proxy-mode").onchange = $("proxy-auth").onchange = updateSettingsControls;
$("show-password").onchange = () =>
  ($("proxy-password").type = $("show-password").checked ? "text" : "password");
$("profiles").onclick = (e) => {
  const b = e.target.closest("[data-action]");
  if (!b) return;
  const id = b.closest("tr").dataset.id;
  if (b.dataset.action === "inspect") inspect(id);
  else
    action(async () => {
      b.disabled = true;
      try {
        await api("/select", { id });
        toast("Профиль применён. Можно проверить соединение.");
      } finally {
        b.disabled = false;
      }
    });
};
for (const b of document.querySelectorAll("[data-test]"))
  b.onclick = () =>
    action(async () => {
      await api("/tests", { id: inspected, kind: b.dataset.test });
      toast("Проверка запущена");
    });
$("cancel-test").onclick = () => action(() => api("/tests/cancel", {}));
$("stop-proxy").onclick = () => action(() => api("/stop", {}));
$("copy-address").onclick = async () => {
  try {
    await navigator.clipboard.writeText(
      "127.0.0.1:" +
        (state.proxy.mode === "mixed"
          ? state.proxy.socksPort
          : state.proxy.httpPort),
    );
    toast("Адрес HTTP-прокси скопирован");
  } catch {
    toast("Адрес: 127.0.0.1:" + state.proxy.socksPort);
  }
};
$("source-form").onsubmit = async (e) => {
  e.preventDefault();
  const b = $("source-submit");
  b.disabled = true;
  b.textContent = "Загружаем…";
  $("source-error").textContent = "";
  try {
    const file = $("source-file").files[0];
    if (file && file.size > 4 * 1024 * 1024)
      throw new Error("Размер JSON-файла не должен превышать 4 MiB");
    await api("/sources", {
      id: $("source-id").value,
      name: $("source-name").value,
      url: $("source-url").value,
      userAgent: $("source-agent").value,
      content: file ? await file.text() : "",
    });
    $("source-dialog").close();
    toast("Профили загружены");
    await refresh();
  } catch (e) {
    $("source-error").textContent = e.message;
  } finally {
    b.disabled = false;
    b.textContent = "Загрузить профили";
  }
};
$("settings-form").onsubmit = async (e) => {
  e.preventDefault();
  const b = e.submitter;
  b.disabled = true;
  $("settings-error").textContent = "";
  try {
    await api("/settings", {
      proxy: {
        mode: $("proxy-mode").value,
        socksPort: Number($("socks-port").value),
        httpPort: Number($("http-port").value),
        auth: $("proxy-auth").checked,
        username: $("proxy-user").value,
        password: $("proxy-password").value,
        udp: $("proxy-udp").checked,
      },
      corePath: $("core-path").value,
      refreshMinutes: Number($("refresh-minutes").value),
    });
    $("settings-dialog").close();
    toast("Настройки сохранены");
    await refresh();
  } catch (e) {
    $("settings-error").textContent = e.message;
  } finally {
    b.disabled = false;
  }
};
$("shutdown").onclick = () => {
  if (confirm("Завершить PortTide и остановить его локальный прокси?"))
    action(async () => {
      await api("/shutdown", {});
      shuttingDown = true;
      $("settings-dialog").close();
      $("notice").textContent = "PortTide завершён. Эту вкладку можно закрыть.";
      show("notice");
      $("runtime-badge").textContent = "Завершён";
      $("runtime-badge").classList.remove("online");
      show("stop-proxy", false);
    });
};
refresh();
setInterval(refresh, 3000);

function friendlyMetadata(meta) {
  const labels = {
    "profile-title": "Название у поставщика",
    "profile-description": "Описание",
    announce: "Сообщение поставщика",
    "support-url": "Поддержка",
    "profile-web-page-url": "Страница подписки",
    "profile-update-interval": "Интервал поставщика, ч",
  };
  const result = {};
  for (const [k, v] of Object.entries(meta)) {
    if (k === "subscription-userinfo") {
      const data = {};
      for (const part of v.split(";")) {
        const [name, value] = part.trim().split("=");
        data[name] = Number(value);
      }
      if (Number.isFinite(data.upload) && Number.isFinite(data.download))
        result["Использовано трафика"] =
          num((data.upload + data.download) / 1e9, 2) + " GB";
      if (Number.isFinite(data.total))
        result["Лимит"] =
          data.total === 0 ? "Без лимита" : num(data.total / 1e9, 2) + " GB";
      if (data.expire > 0)
        result["Срок действия"] = new Date(data.expire * 1000).toLocaleString();
    } else {
      result[labels[k] || k] = v;
    }
  }
  return result;
}
