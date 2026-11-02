"use strict";
const $ = (id) => document.getElementById(id);
let accessToken = document.querySelector('meta[name="porttide-token"]').content;
let state = null,
  inspected = "",
  shuttingDown = false,
  busy = false,
  lastSignature = "",
  toastTimer,
  refreshing = false,
  disconnects = 0;
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
async function api(path, body, method, retry = true) {
  const opts = {
    method: method || (body === undefined ? "GET" : "POST"),
    headers: { "X-PortTide-Token": accessToken },
  };
  if (opts.method !== "GET") {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body || {});
  }
  const response = await fetch("/api" + path, opts);
  if (response.status === 401 && retry) {
    const page = await fetch("/", { cache: "no-store" });
    const doc = new DOMParser().parseFromString(await page.text(), "text/html");
    const token = doc.querySelector('meta[name="porttide-token"]')?.content;
    if (token) { accessToken = token; return api(path, body, method, false); }
  }
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
  if (refreshing) return;
  refreshing = true;
  try {
    const next = await api("/state");
    if (shuttingDown && next.instanceId === state?.instanceId) return;
    const signature = JSON.stringify(next);
    state = next;
    disconnects = 0;
    shuttingDown = false;
    show("connection-notice", false);
    if (signature !== lastSignature) {
      lastSignature = signature;
      render();
    }
  } catch (e) {
    disconnects++;
    $("connection-notice").textContent = shuttingDown
      ? "PortTide завершён. Для запуска используйте ярлык приложения."
      : disconnects < 4 ? "Переподключение…" : "PortTide недоступен. Если приложение закрыто, запустите его через ярлык. Панель подключится автоматически.";
    show("connection-notice");
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
  const active = findProfile(state.active);
  labelWithFlags(
    $("active-name"),
    state.running && active ? active.name : "Прокси выключен",
  );
  $("runtime-badge").classList.toggle("online", state.running);
  show("connection-details", state.sources.length > 0 || state.running);
  show("stop-proxy", state.running);
  const p = state.proxy;
  $("proxy-address").textContent =
    p.mode === "mixed"
      ? "HTTP / SOCKS5 · 127.0.0.1:" + p.socksPort
      : "SOCKS5 :" + p.socksPort + " · HTTP :" + p.httpPort;
  $("auth-label").textContent = p.auth ? "С авторизацией" : "Без авторизации";
  const notice =
    state.notice ||
    (!state.coreAvailable ? "Xray не найден. Укажите путь в настройках." : "");
  $("notice").textContent = notice;
  show("notice", Boolean(notice));
  renderSources();
  renderSubscriptionSummary();
  renderRows();
  renderDiagnostics();
  renderProgress();
  renderUpdates();
}
function subscriptionUsage(meta) {
  const fields = {};
  for (const part of (meta["subscription-userinfo"] || "").split(";")) {
    const [key, value] = part.trim().split("=");
    if (value !== undefined && value !== "" && Number.isFinite(Number(value)))
      fields[key] = Number(value);
  }
  return fields;
}
function renderSubscriptionSummary() {
  const box = $("subscription-summary"),
    filter = $("source-filter").value;
  const expanded = new Map(
    Array.from(box.children, (e) => [e.dataset.source, e.open]),
  );
  box.replaceChildren();
  const sources = state.sources.filter((s) => !filter || s.id === filter);
  show("subscription-summary", sources.length > 0);
  const active = findProfile(state.active || state.selected);
  for (const [i, source] of sources.entries()) {
    const m = source.metadata || {},
      usage = subscriptionUsage(m),
      card = document.createElement("details");
    card.className = "subscription-card";
    card.dataset.source = source.id;
    card.open = expanded.has(source.id)
      ? expanded.get(source.id)
      : sources.length === 1 ||
        active?.sourceId === source.id ||
        (!active && i === 0);
    const summary = text("summary", "", "subscription-heading"),
      title = text("strong", source.name, "subscription-title"),
      badges = text("span", "", "subscription-badges");
    if (Number.isFinite(usage.upload) && Number.isFinite(usage.download)) {
      let quota = num((usage.upload + usage.download) / 1e9, 1) + " ГБ";
      if (Number.isFinite(usage.total))
        quota +=
          " / " + (usage.total === 0 ? "∞" : num(usage.total / 1e9, 1) + " ГБ");
      badges.append(text("span", quota, "subscription-badge"));
    }
    if (usage.expire > 0) {
      const expires = new Date(usage.expire * 1000);
      if (!Number.isNaN(expires.getTime()))
        badges.append(
          text(
            "span",
            "до " + expires.toLocaleDateString(),
            "subscription-badge" + (expires < Date.now() ? " expired" : ""),
          ),
        );
    }
    summary.append(title, badges);
    card.append(summary);
    const notes = [
      ...new Set([m["announce"], m["profile-description"]].filter(Boolean)),
    ];
    for (const note of notes) {
      const paragraph = text("p", "", "subscription-announcement");
      labelWithFlags(paragraph, note);
      card.append(paragraph);
    }
    if (!notes.length)
      card.append(
        text(
          "p",
          "Описание не передано поставщиком.",
          "muted small subscription-announcement",
        ),
      );
    if (source.error)
      card.append(
        text("p", source.error, "result-error subscription-announcement"),
      );
    box.append(card);
  }
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
        s.error ? "Ошибка обновления · кеш сохранён" : date(s.updatedAt),
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
    profiles.length === state.profiles.length
      ? "Профилей: " + profiles.length
      : "Найдено: " + profiles.length + " из " + state.profiles.length;
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
    labelWithFlags(row.querySelector(".profile-name"), p.name);
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
    if (latency && !latency.error)
      row.querySelector(".latency-cell").textContent =
        num(latency.value, 0) + " мс";
    const geo =
      state.results[p.id + ":geo"]?.measurements.filter(
        (m) => m.kind === "observer" && !m.error,
      ) || [];
    if (geo.length)
      countryCell(row.querySelector(".geo-cell"), [
        ...new Set(geo.map((m) => m.country || m.ip)),
      ]);
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
  const dialog = $("diagnostics-dialog");
  if (!dialog.open) dialog.showModal();
}
function renderProgress() {
  if (!state) return;
  const job = state.job,
    progress = job.progress || {};
  show("test-progress", job.running);
  show("test-progress-bar", job.running);
  show("cancel-test", job.running);
  show("open-test-job", Boolean(job.id && (job.running || job.result)));
  if (!job.running) {
    if (job.result)
      $("open-test-job").textContent =
        "✓ " + (kindName[job.kind] || "Проверка");
    return;
  }
  const phases = {
    preparing: "Подготовка",
    "latency-warmup": "Прогрев · задержка",
    latency: "Задержка",
    geo: "IP и страна",
    "download-warmup": "Прогрев · скачивание",
    download: "Скачивание",
    "upload-warmup": "Прогрев · отдача",
    upload: "Отдача",
  };
  const elapsed = job.startedAt
    ? Math.max(0, Math.floor((Date.now() - Date.parse(job.startedAt)) / 1000))
    : 0;
  const count = progress.total
    ? " · " + progress.completed + "/" + progress.total
    : "";
  $("progress-text").textContent =
    (phases[progress.phase] || kindName[job.kind] || "Проверка") +
    count +
    " · " +
    elapsed +
    " с";
  $("open-test-job").textContent = "◷ " + (kindName[job.kind] || "Проверка");
  const bar = $("test-progress-bar");
  if (progress.total) {
    bar.value = (100 * progress.completed) / progress.total;
  } else if (progress.durationSeconds && progress.startedAt) {
    bar.value = Math.min(
      98,
      (100 * (Date.now() - Date.parse(progress.startedAt))) /
        1000 /
        progress.durationSeconds,
    );
  } else {
    bar.removeAttribute("value");
  }
}

function renderDiagnostics() {
  if (!state) return;
  const p = findProfile(inspected);
  labelWithFlags($("diagnostic-title"), p ? p.name : "Проверка профиля");
  show("test-actions", Boolean(p));
  renderProgress();
  for (const button of document.querySelectorAll("[data-test]"))
    button.disabled =
      !p?.supported || !state.coreAvailable || state.job.running;
  const results = $("test-results");
  results.replaceChildren();
  let count = 0;
  for (const kind of ["latency", "speed", "geo"]) {
    const result = state.results[inspected + ":" + kind];
    if (!result) continue;
    count++;
    const block = text("section", "", "result-block"),
      head = text("div", "", "result-heading");
    head.append(
      text("h3", kindName[kind]),
      text("span", date(result.at), "muted small"),
    );
    block.append(head);
    if (result.error) block.append(text("p", result.error, "result-error"));
    if (kind === "latency") renderLatency(block, result);
    else if (kind === "speed") renderSpeed(block, result);
    else renderGeography(block, result);
    results.append(block);
  }
  show("diagnostic-empty", !count && !state.job.running);
}
function metricCard(label, value, unit, subtitle) {
  const card = text("div", "", "metric primary-metric");
  card.append(
    text("small", label),
    text("div", value + (unit ? " " + unit : ""), "metric-value"),
  );
  if (subtitle) card.append(text("small", subtitle));
  return card;
}
function renderLatency(block, result) {
  const summary = result.measurements.find((m) => m.name === "Median"),
    samples = result.measurements.filter((m) => m.name !== "Median");
  if (!summary) return;
  if (summary.error) {
    block.append(text("p", summary.error, "result-error"));
    return;
  }
  const grid = text("div", "", "result-grid");
  grid.append(
    metricCard(
      "Медиана",
      num(summary.value, 0),
      "мс",
      summary.samples
        ? "Диапазон " + num(summary.min, 0) + "–" + num(summary.max, 0) + " мс"
        : "",
    ),
  );
  const successful = summary.samples ?? samples.filter((m) => !m.error).length,
    total = successful + (summary.failures || 0);
  grid.append(
    metricCard(
      "Ответы",
      successful + " / " + total,
      "",
      summary.jitter !== undefined
        ? "Колебания " + num(summary.jitter, 1) + " мс"
        : "",
    ),
  );
  block.append(grid);
  const detail = document.createElement("details");
  detail.className = "sample-details";
  detail.append(text("summary", "Все замеры"));
  const list = text("div", "");
  for (const [i, m] of samples.entries())
    list.append(
      text(
        "div",
        i + 1 + ". " + (m.error || num(m.value, 1) + " мс"),
        m.error ? "result-error" : "",
      ),
    );
  detail.append(list);
  block.append(detail);
}
function renderSpeed(block, result) {
  const grid = text("div", "", "result-grid");
  for (const m of result.measurements) {
    const label = m.kind === "download" ? "Скачивание ↓" : "Отдача ↑";
    let card;
    if (m.error) {
      card = text("div", "", "metric");
      card.append(text("small", label), text("p", m.error, "result-error"));
    } else {
      card = metricCard(
        label,
        num(m.value, 1),
        "Мбит/с",
        num(m.seconds, 1) + " с · " + (m.streams || 1) + " потока",
      );
      if (m.warning) card.append(text("p", m.warning, "result-warning"));
    }
    grid.append(card);
  }
  block.append(grid);
  const details = document.createElement("details");
  details.className = "sample-details";
  details.append(text("summary", "Объём и методика"));
  details.append(
    text(
      "p",
      "Cloudflare. Прогрев исключён из расчёта; для отдачи учитываются передачи с подтверждённым завершением.",
    ),
  );
  for (const m of result.measurements) {
    if (m.trafficBytes)
      details.append(
        text(
          "div",
          (m.kind === "download" ? "↓ " : "↑ ") +
            num(m.trafficBytes / 1048576, 1) +
            " МиБ с прогревом · " +
            (m.samples || 0) +
            " завершённых передач",
        ),
      );
  }
  block.append(details);
}
function renderGeography(block, result) {
  const wrap = text("div", "", "table-scroll"),
    table = text("table", "", "geo-table"),
    thead = document.createElement("thead"),
    header = document.createElement("tr");
  for (const h of ["Источник", "IP", "Страна", "Провайдер / ASN"])
    header.append(text("th", h));
  thead.append(header);
  table.append(thead);
  const body = document.createElement("tbody");
  for (const m of result.measurements) {
    const row = document.createElement("tr"),
      country = text("td", "");
    if (m.error) {
      country.textContent = m.error;
      country.className = "result-error";
    } else {
      countryCell(country, m.country ? [m.country] : []);
      if (m.region) country.append(text("div", m.region, "muted small"));
    }
    row.append(
      text("td", (m.kind === "observer" ? "Сайт · " : "База · ") + m.name),
      text("td", m.ip || "—"),
      country,
      text("td", [m.organization, m.asn].filter(Boolean).join(" · ") || "—"),
    );
    body.append(row);
  }
  table.append(body);
  wrap.append(table);
  block.append(wrap);
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
  $("http-port").value = p.httpPort || "";
  $("proxy-auth").checked = p.auth;
  $("proxy-user").value = p.username;
  $("proxy-password").value = p.password;
  $("proxy-password").type = "password";
  $("show-password").checked = false;
  $("proxy-udp").checked = p.udp;
  $("settings-error").textContent = "";
  updateSettingsControls();
  $("settings-dialog").showModal();
}
function updateSettingsControls() {
  const mixed = $("proxy-mode").value === "mixed";
  show("http-port-field", !mixed);
  $("http-port").disabled = mixed;
  $("http-port").required = !mixed;
  $("socks-port-title").textContent = mixed
    ? "Порт HTTP / SOCKS5"
    : "Порт SOCKS5";
  $("proxy-port-fields").classList.toggle("single-port", mixed);
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
$("source-filter").onchange = () => {
  renderRows();
  renderSubscriptionSummary();
};
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
        toast("Прокси запущен");
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
      corePath: state.corePath,
      refreshMinutes: state.refreshMinutes,
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
      $("app-settings-dialog").close();
      $("notice").textContent = "PortTide завершён. Эту вкладку можно закрыть.";
      show("notice");
      $("runtime-badge").textContent = "Завершён";
      $("runtime-badge").classList.remove("online");
      show("stop-proxy", false);
    });
};
refresh().then(() => { if (location.hash === "#settings") appSettingsDialog(); });
window.addEventListener("hashchange", () => { if (location.hash === "#settings") appSettingsDialog(); });
setInterval(refresh, 2000);
setInterval(renderProgress, 1000);
$("open-test-job").onclick = () => {
  if (state?.job?.profileId) inspect(state.job.profileId);
};
$("diagnostics-dialog").addEventListener("close", () => {
  const row = Array.from($("profiles").children).find(
    (r) => r.dataset.id === inspected,
  );
  row?.querySelector("[data-action=inspect]")?.focus({ preventScroll: true });
});

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

function appSettingsDialog() {
  if (!state) return;
  const d = state.desktop;
  $("start-at-login").checked = d.startAtLogin;
  $("start-at-login").disabled = !state.desktopSupported;
  $("connect-on-launch").checked = d.connectOnLaunch;
  $("background-launch").checked = d.background;
  $("core-path").value = state.corePath;
  $("core-path").placeholder = state.coreResolved;
  $("refresh-minutes").value = state.refreshMinutes;
  $("app-settings-error").textContent = "";
  renderUpdates();
  if (!$("app-settings-dialog").open) $("app-settings-dialog").showModal();
}
function renderUpdates() {
  const u = state.updates || {};
  show("update-notice", !!u.available);
  $("update-notice-text").textContent = u.available ? "Доступна версия " + u.version : "";
  $("update-status").textContent = u.error || u.phase || (u.checking ? "Проверяем…" : u.available ? "Доступна версия " + u.version : u.checkedAt && !u.checkedAt.startsWith("0001") ? "Установлена актуальная версия" : "Текущая версия: " + state.version);
  $("check-updates").disabled = !!(u.checking || u.installing);
  show("release-notes", !!u.page);
  if (u.page) $("release-notes").href = u.page;
  show("install-update", !!(u.available && u.canInstall));
  $("install-update").disabled = !!(u.checking || u.installing || state.job.running);
  show("update-restart-hint", !!(u.available && u.canInstall));
}
$("open-app-settings").onclick = $("open-update").onclick = appSettingsDialog;
$("check-updates").onclick = () => action(() => api("/updates/check", {}));
$("install-update").onclick = () => action(() => api("/updates/install", {}));
$("app-settings-form").onsubmit = async (e) => {
  e.preventDefault();
  const button = e.submitter;
  button.disabled = true;
  $("app-settings-error").textContent = "";
  try {
    await api("/desktop", {
      desktop: {startAtLogin: $("start-at-login").checked, connectOnLaunch: $("connect-on-launch").checked, background: $("background-launch").checked},
      corePath: $("core-path").value,
      refreshMinutes: Number($("refresh-minutes").value),
    });
    $("app-settings-dialog").close();
    toast("Настройки сохранены");
    await refresh();
  } catch (error) { $("app-settings-error").textContent = error.message; }
  finally { button.disabled = false; }
};
