const $ = selector => document.querySelector(selector);
const api = async (path, options = {}) => {
  const response = await fetch(path, {headers: {'Content-Type': 'application/json'}, ...options});
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `Ошибка ${response.status}`);
  return data;
};
const notify = (message, bad = false) => {
  const notice = $('#notice');
  notice.textContent = message;
  notice.classList.toggle('bad', bad);
  setTimeout(() => { notice.textContent = ''; notice.classList.remove('bad'); }, 6000);
};
const text = id => $(id).value.trim();
const number = id => Number($(id).value || 0);
const set = (id, value) => { $(id).value = value ?? ''; };
const formatGiB = bytes => bytes ? `${(bytes / (1024 ** 3)).toFixed(1)} ГБ` : '0 ГБ';

let instances = [];
let jitsiResources = [];
let instancesFingerprint = '';
let activeInstanceId = '';
let pendingNewInstanceId = '';
let diagnosticsData = null;
let activeLogService = 'webgui';

async function boot() {
  try {
    const me = await api('/api/me');
    $('#login').hidden = true;
    $('#panel').hidden = false;
    $('#currentUser').textContent = me.username;
    await Promise.all([loadInstances(), loadStatus(), loadJitsiResources()]);
  } catch {
    $('#login').hidden = false;
    $('#panel').hidden = true;
  }
}

$('#loginForm').addEventListener('submit', async event => {
  event.preventDefault();
  $('#loginError').textContent = '';
  try {
    await api('/api/login', {method: 'POST', body: JSON.stringify({username: text('#username'), password: $('#password').value})});
    await boot();
  } catch (error) { $('#loginError').textContent = error.message; }
});
$('#logout').onclick = async () => { await api('/api/logout', {method: 'POST'}); location.reload(); };

async function loadInstances() {
  const result = await api('/api/instances');
  const nextInstances = result.instances || [];
  const fingerprint = JSON.stringify(nextInstances);
  instances = nextInstances;
  if (!instances.some(instance => instance.id === activeInstanceId)) activeInstanceId = instances[0]?.id || '';
  if (fingerprint !== instancesFingerprint) {
    instancesFingerprint = fingerprint;
    renderInstances();
  }
}
function currentInstance() { return instances.find(instance => instance.id === activeInstanceId); }

function matchingJitsiResource(roomUrl) {
  return jitsiResources.find(resource => roomUrl === resource || roomUrl.startsWith(`${resource}/`)) || '';
}
function renderJitsiResourceOptions(roomUrl = text('#roomId')) {
  const select = $('#jitsiResource');
  const selected = matchingJitsiResource(roomUrl);
  const options = jitsiResources.map(resource => {
    const option = document.createElement('option'); option.value = resource; option.textContent = resource; return option;
  });
  const manual = document.createElement('option'); manual.value = '__manual__'; manual.textContent = 'Ввести URL вручную'; options.push(manual);
  select.replaceChildren(...options); select.value = selected || '__manual__';
}
async function loadJitsiResources() {
  const result = await api('/api/jitsi-resources');
  jitsiResources = result.resources || [];
  renderJitsiResourceOptions();
}

function fillSettings(s) {
  set('#mode', s.mode); set('#dataDir', s.dataDir); $('#debug').checked = Boolean(s.debug);
  set('#provider', s.provider); set('#providerToken', s.providerToken); set('#transport', s.transport);
  set('#roomId', s.roomId); set('#roomChannel', s.roomChannel); set('#cryptoKey', s.cryptoKey);
  set('#cryptoKeyFile', s.cryptoKeyFile); set('#dns', s.dns);
  set('#engineName', s.engine?.name); set('#engineUrl', s.engine?.url); set('#engineToken', s.engine?.token);
  set('#socksHost', s.socks?.host); set('#socksPort', s.socks?.port); set('#socksUser', s.socks?.user); set('#socksPass', s.socks?.pass);
  set('#proxyAddr', s.socks?.proxyAddr); set('#proxyPort', s.socks?.proxyPort); set('#proxyUser', s.socks?.proxyUser); set('#proxyPass', s.socks?.proxyPass);
  set('#videoCodec', s.video?.codec); set('#videoWidth', s.video?.width); set('#videoHeight', s.video?.height); set('#videoFps', s.video?.fps);
  set('#videoQrSize', s.video?.qrSize); set('#videoQrRecovery', s.video?.qrRecovery); set('#videoTileModule', s.video?.tileModule); set('#videoTileRs', s.video?.tileRs);
  set('#vp8Fps', s.vp8?.fps); set('#vp8BatchSize', s.vp8?.batchSize);
  set('#seiFps', s.sei?.fps); set('#seiBatchSize', s.sei?.batchSize); set('#seiFragmentSize', s.sei?.fragmentSize); set('#seiAckTimeoutMs', s.sei?.ackTimeoutMs);
  set('#livenessInterval', s.liveness?.interval); set('#livenessTimeout', s.liveness?.timeout); set('#livenessFailures', s.liveness?.failures);
  set('#maxSessionDuration', s.lifecycle?.maxSessionDuration); set('#maxPayloadSize', s.traffic?.maxPayloadSize);
  set('#minDelay', s.traffic?.minDelay); set('#maxDelay', s.traffic?.maxDelay);
}

function formSettings() {
  return {
    mode: text('#mode'), provider: text('#provider'), providerToken: $('#providerToken').value.trim(), transport: text('#transport'),
    roomId: text('#roomId'), roomChannel: text('#roomChannel'), cryptoKey: text('#cryptoKey'), cryptoKeyFile: text('#cryptoKeyFile'),
    dns: text('#dns'), dataDir: text('#dataDir'), debug: $('#debug').checked,
    engine: {name: text('#engineName'), url: text('#engineUrl'), token: $('#engineToken').value.trim()},
    socks: {host: text('#socksHost'), port: number('#socksPort'), user: text('#socksUser'), pass: $('#socksPass').value,
      proxyAddr: text('#proxyAddr'), proxyPort: number('#proxyPort'), proxyUser: text('#proxyUser'), proxyPass: $('#proxyPass').value},
    video: {codec: text('#videoCodec'), width: number('#videoWidth'), height: number('#videoHeight'), fps: number('#videoFps'),
      qrSize: number('#videoQrSize'), qrRecovery: text('#videoQrRecovery'), tileModule: number('#videoTileModule'), tileRs: number('#videoTileRs')},
    vp8: {fps: number('#vp8Fps'), batchSize: number('#vp8BatchSize')},
    sei: {fps: number('#seiFps'), batchSize: number('#seiBatchSize'), fragmentSize: number('#seiFragmentSize'), ackTimeoutMs: number('#seiAckTimeoutMs')},
    liveness: {interval: text('#livenessInterval'), timeout: text('#livenessTimeout'), failures: number('#livenessFailures')},
    lifecycle: {maxSessionDuration: text('#maxSessionDuration')},
    traffic: {maxPayloadSize: number('#maxPayloadSize'), minDelay: text('#minDelay'), maxDelay: text('#maxDelay')},
    failover: {retryDelay: '2s', maxCycles: 0}
  };
}

function providerLabel(provider) { return {jitsi: 'Jitsi', telemost: 'Яндекс Телемост', wbstream: 'WB Stream', none: 'Прямое подключение'}[provider] || provider; }
function transportLabel(transport) { return {datachannel: 'Data channel', vp8channel: 'VP8 channel', seichannel: 'SEI channel', videochannel: 'Video channel'}[transport] || transport; }
function actionButton(label, className, handler, title = '', disabled = false) {
  const button = document.createElement('button');
  button.type = 'button'; button.className = className; button.textContent = label; button.title = title; button.disabled = disabled; button.onclick = handler;
  return button;
}

function renderInstances() {
  const list = $('#instanceList');
  if (!instances.length) {
    const empty = document.createElement('p'); empty.className = 'hint instance-empty'; empty.textContent = 'Экземпляров пока нет. Создайте первый сервер.';
    list.replaceChildren(empty); return;
  }
  list.replaceChildren(...instances.map((instance, index) => {
    const card = document.createElement('section'); card.className = 'profile-card instance-card';
    const order = document.createElement('span'); order.className = `profile-order ${instance.active ? 'online' : ''}`; order.textContent = instance.active ? '●' : String(index + 1);
    const summary = document.createElement('div'); summary.className = 'profile-summary';
    const titleRow = document.createElement('div'); titleRow.className = 'instance-title-row';
    const name = document.createElement('h3'); name.textContent = instance.name;
    const state = document.createElement('span'); state.className = `instance-state ${instance.active ? 'online' : instance.status === 'ошибка' ? 'failed' : ''}`; state.textContent = instance.status;
    titleRow.append(name, state);
    const meta = document.createElement('p'); meta.textContent = `${providerLabel(instance.settings.provider)} · ${transportLabel(instance.settings.transport)} · ${instance.unit}`;
    const room = document.createElement('small'); room.textContent = instance.settings.roomId || 'Комната не указана';
    const config = document.createElement('small'); config.className = 'instance-config'; config.textContent = instance.configPath;
    summary.append(titleRow, meta, room, config);
    const actions = document.createElement('div'); actions.className = 'profile-card-actions instance-actions';
    actions.append(
      actionButton('Конфигурация', 'secondary', () => openInstanceSettings(instance.id)),
      actionButton('Подключение клиента', '', () => openClientConnection(instance.id)),
      actionButton(instance.active ? 'Перезапустить' : 'Запустить', 'text secondary', () => runInstance(instance.id, instance.active ? 'restart' : 'start')),
      actionButton('Остановить', 'text danger', () => runInstance(instance.id, 'stop'), '', !instance.active),
      actionButton('Логи', 'text secondary', () => openInstanceLogs(instance.id)),
      actionButton('Дублировать', 'text secondary', () => duplicateInstance(instance.id)),
      actionButton('Удалить', 'text danger', () => deleteInstance(instance.id))
    );
    card.append(order, summary, actions); return card;
  }));
}

async function runInstance(id, action) {
  try {
    await api(`/api/instances/${encodeURIComponent(id)}/service/${action}`, {method: 'POST'});
    notify('Команда выполнена.'); await Promise.all([loadInstances(), loadStatus()]);
  } catch (error) { notify(error.message, true); }
}

function openInstanceSettings(id, isNew = false) {
  const instance = instances.find(item => item.id === id); if (!instance) return;
  activeInstanceId = id; pendingNewInstanceId = isNew ? id : '';
  set('#profileName', instance.name); fillSettings(instance.settings); $('#settingsDialogTitle').textContent = instance.name;
  renderJitsiResourceOptions(instance.settings.roomId); refreshForm(); $('#profileSettingsDialog').showModal();
}
async function closeInstanceSettings() {
  const id = pendingNewInstanceId; pendingNewInstanceId = ''; $('#profileSettingsDialog').close();
  if (id) {
    try { await api(`/api/instances/${encodeURIComponent(id)}`, {method: 'DELETE'}); }
    catch (error) { notify(error.message, true); }
  }
  await loadInstances().catch(error => notify(error.message, true));
}

$('#settingsForm').addEventListener('submit', async event => {
  event.preventDefault(); const instance = currentInstance(); if (!instance) return;
  try {
    const name = text('#profileName');
    await api(`/api/instances/${encodeURIComponent(instance.id)}`, {method: 'PUT', body: JSON.stringify({name, settings: formSettings()})});
    pendingNewInstanceId = ''; $('#profileSettingsDialog').close();
    notify(`Сервер «${name}» сохранён. Перезапустите его, если он уже работает.`);
    await Promise.all([loadInstances(), loadStatus()]);
  } catch (error) { notify(error.message, true); }
});
$('#closeSettings').onclick = closeInstanceSettings;
$('#cancelSettings').onclick = closeInstanceSettings;
$('#profileSettingsDialog').addEventListener('cancel', event => { event.preventDefault(); closeInstanceSettings(); });

async function generateJitsiRoom() {
  const resource = text('#jitsiResource');
  if (!resource || resource === '__manual__') return notify('Сначала выберите сохранённый Jitsi-ресурс.', true);
  const button = $('#generateJitsiRoom'); button.disabled = true;
  try {
    const result = await api('/api/jitsi-room', {method: 'POST', body: JSON.stringify({resource})});
    set('#roomId', result.roomUrl); notify('Новый адрес комнаты создан. Она откроется при первом подключении.');
  } catch (error) { notify(error.message, true); }
  finally { button.disabled = false; }
}
$('#jitsiResource').addEventListener('change', () => { if (text('#jitsiResource') !== '__manual__') generateJitsiRoom(); });
$('#generateJitsiRoom').onclick = generateJitsiRoom;
$('#roomId').addEventListener('input', () => {
  if (!matchingJitsiResource(text('#roomId'))) $('#jitsiResource').value = '__manual__';
});

function closeJitsiResources() { $('#jitsiResourcesDialog').close(); }
$('#manageJitsiResources').onclick = () => {
  set('#jitsiResourcesInput', jitsiResources.join('\n')); $('#jitsiResourcesDialog').showModal();
};
$('#closeJitsiResources').onclick = closeJitsiResources;
$('#cancelJitsiResources').onclick = closeJitsiResources;
$('#jitsiResourcesDialog').addEventListener('cancel', event => { event.preventDefault(); closeJitsiResources(); });
$('#jitsiResourcesForm').addEventListener('submit', async event => {
  event.preventDefault();
  const resources = $('#jitsiResourcesInput').value.split(/\r?\n/).map(value => value.trim()).filter(Boolean);
  try {
    const result = await api('/api/jitsi-resources', {method: 'PUT', body: JSON.stringify({resources})});
    jitsiResources = result.resources || []; renderJitsiResourceOptions(); closeJitsiResources(); notify('Список Jitsi-ресурсов сохранён.');
  } catch (error) { notify(error.message, true); }
});

function uniqueInstanceName(base) { let candidate = base, suffix = 2; while (instances.some(instance => instance.name === candidate)) candidate = `${base} ${suffix++}`; return candidate; }
$('#addInstance').onclick = async () => {
  try {
    const created = await api('/api/instances', {method: 'POST', body: JSON.stringify({name: uniqueInstanceName('Новый сервер')})});
    await loadInstances(); openInstanceSettings(created.id, true);
  } catch (error) { notify(error.message, true); }
};
async function duplicateInstance(id) {
  const source = instances.find(instance => instance.id === id); if (!source) return;
  try {
    const created = await api('/api/instances', {method: 'POST', body: JSON.stringify({name: uniqueInstanceName(`${source.name} — копия`), settings: source.settings})});
    await loadInstances(); openInstanceSettings(created.id, true);
  } catch (error) { notify(error.message, true); }
}
async function deleteInstance(id) {
  const instance = instances.find(item => item.id === id);
  if (!instance || !confirm(`Остановить и удалить сервер «${instance.name}»? Его YAML будет удалён.`)) return;
  try {
    await api(`/api/instances/${encodeURIComponent(id)}`, {method: 'DELETE'}); notify('Экземпляр удалён.');
    await Promise.all([loadInstances(), loadStatus()]);
  } catch (error) { notify(error.message, true); }
}

function clearShare() { set('#clientUri', ''); $('#qrCode').removeAttribute('src'); $('#shareResult').hidden = true; $('#shareLoading').hidden = false; }
async function openClientConnection(id) {
  const instance = instances.find(item => item.id === id); if (!instance) return;
  activeInstanceId = id; set('#shareComment', instance.name); $('#clientDialogTitle').textContent = instance.name;
  clearShare(); $('#clientDialog').showModal(); await generateShare();
}
async function generateShare() {
  const instance = currentInstance(); if (!instance) return;
  const button = $('#regenerateShare'); button.disabled = true;
  $('#shareLoading').hidden = false; $('#shareLoading').textContent = 'Создаём ссылку и QR-код…'; $('#shareLoading').classList.remove('bad'); $('#shareResult').hidden = true;
  try {
    const result = await api(`/api/instances/${encodeURIComponent(instance.id)}/share`, {method: 'POST', body: JSON.stringify({comment: text('#shareComment')})});
    set('#clientUri', result.uri); $('#qrCode').src = result.qr; $('#shareLoading').hidden = true; $('#shareResult').hidden = false;
  } catch (error) { $('#shareLoading').textContent = error.message; $('#shareLoading').classList.add('bad'); }
  finally { button.disabled = false; }
}
$('#regenerateShare').onclick = generateShare;
$('#closeClient').onclick = () => $('#clientDialog').close();
$('#clientDialog').addEventListener('close', () => $('#shareLoading').classList.remove('bad'));
$('#copyClientUri').onclick = async () => {
  const uri = text('#clientUri'); if (!uri) return notify('Сначала создайте клиентскую ссылку.', true);
  try { await navigator.clipboard.writeText(uri); notify('Клиентская ссылка скопирована.'); }
  catch { notify('Не удалось скопировать автоматически. Выделите ссылку вручную.', true); }
};

function refreshForm() {
  const mode = text('#mode'), provider = text('#provider'), transport = text('#transport');
  $('#clientSocksGroup').hidden = mode !== 'cnc'; $('#serverSocksGroup').hidden = mode !== 'srv'; $('#engineGroup').hidden = provider !== 'none'; $('#jitsiRoomTools').hidden = provider !== 'jitsi';
  document.querySelector('.provider-token').hidden = provider !== 'wbstream'; $('#vp8Group').hidden = transport !== 'vp8channel';
  $('#seiGroup').hidden = transport !== 'seichannel'; $('#videoGroup').hidden = transport !== 'videochannel';
  for (const option of $('#transport').options) option.disabled = provider === 'telemost' && (option.value === 'datachannel' || option.value === 'seichannel');
  if (provider === 'telemost' && (transport === 'datachannel' || transport === 'seichannel')) { $('#transport').value = 'vp8channel'; return refreshForm(); }
  const hints = {'jitsi:datachannel': 'Рекомендуемая и самая быстрая комбинация. Для Jitsi укажите полный URL комнаты.',
    'wbstream:datachannel': 'Нужен токен аккаунта или модератора с canPublishData=true на обеих сторонах.',
    'telemost:vp8channel': 'Стабильная комбинация для Телемоста.', 'jitsi:seichannel': 'Работает, но на загруженных Jitsi datachannel или vp8channel надёжнее.',
    'telemost:videochannel': 'Работает медленно; используйте vp8channel, если возможно.'};
  $('#combinationHint').textContent = hints[`${provider}:${transport}`] || 'Параметры провайдера и транспорта должны совпадать на сервере и клиенте.';
}
$('#mode').addEventListener('change', refreshForm); $('#provider').addEventListener('change', refreshForm); $('#transport').addEventListener('change', refreshForm);
$('#videoCodec').addEventListener('change', () => { if (text('#videoCodec') === 'tile') { set('#videoWidth', 1080); set('#videoHeight', 1080); } });
$('#cryptoKey').addEventListener('input', () => { if (text('#cryptoKey')) set('#cryptoKeyFile', ''); });
$('#cryptoKeyFile').addEventListener('input', () => { if (text('#cryptoKeyFile')) set('#cryptoKey', ''); });
$('#generateKey').onclick = () => {
  const bytes = crypto.getRandomValues(new Uint8Array(32)); set('#cryptoKey', Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')); set('#cryptoKeyFile', '');
  notify('Новый ключ создан. Скопируйте его на вторую сторону туннеля.');
};
$('#copyKey').onclick = async () => {
  const key = text('#cryptoKey'); if (!key) return notify('Сначала создайте или введите ключ.', true);
  try { await navigator.clipboard.writeText(key); notify('Ключ скопирован.'); } catch { notify('Не удалось скопировать автоматически. Выделите ключ вручную.', true); }
};

async function loadStatus() {
  try {
    const s = await api('/api/status'); $('#statusText').textContent = s.status; $('#version').textContent = s.version || '-'; $('#currentVersion').textContent = s.version || '-';
    $('#projectDir').textContent = s.projectDir; $('#configPath').textContent = s.configPath; $('#memoryTotal').textContent = formatGiB(s.memoryTotal); $('#swapTotal').textContent = formatGiB(s.swapTotal);
    $('#memoryHint').hidden = !s.swapRecommended;
    $('#topStatus').textContent = s.runningCount ? `● Работает: ${s.runningCount} из ${s.instanceCount}` : (s.installed ? '○ Все серверы остановлены' : '○ Требуется установка');
    $('#topStatus').classList.toggle('inactive', !s.active);
  } catch (error) { notify(error.message, true); }
}
document.querySelectorAll('[data-all-action]').forEach(button => {
  button.onclick = async () => {
    button.disabled = true;
    try { await api(`/api/service/${button.dataset.allAction}`, {method: 'POST'}); notify('Команда выполнена для всех экземпляров.'); await Promise.all([loadInstances(), loadStatus()]); }
    catch (error) { notify(error.message, true); } finally { button.disabled = false; }
  };
});

$('#checkUpdate').onclick = async () => {
  const button = $('#checkUpdate'); button.disabled = true;
  try {
    const update = await api('/api/update/check', {method: 'POST'}); $('#currentVersion').textContent = update.current || 'не установлена'; $('#latestVersion').textContent = update.latest;
    $('#installUpdate').disabled = !update.available; notify(update.needsInstall ? 'Требуется сборка и установка шаблона сервисов' : (update.available ? 'Доступна новая версия' : 'Установлена актуальная версия'));
  } catch (error) { notify(error.message, true); } finally { button.disabled = false; }
};
$('#installUpdate').onclick = async () => {
  const button = $('#installUpdate'); button.disabled = true; button.textContent = 'Устанавливаем...';
  try { const result = await api('/api/update/install', {method: 'POST'}); notify(result.message); await Promise.all([loadInstances(), loadStatus()]); }
  catch (error) { notify(error.message, true); } finally { button.textContent = 'Установить / обновить'; button.disabled = false; }
};

document.querySelectorAll('[data-scroll]').forEach(button => { button.onclick = () => { setActiveNavigation(button); document.getElementById(button.dataset.scroll).scrollIntoView({behavior: 'smooth', block: 'start'}); }; });
function setActiveNavigation(active) { document.querySelectorAll('nav button').forEach(button => button.classList.toggle('active', button === active)); }
function formatUptime(seconds) { if (!seconds) return '—'; const days = Math.floor(seconds / 86400), hours = Math.floor((seconds % 86400) / 3600), minutes = Math.floor((seconds % 3600) / 60); return [days && `${days} д`, hours && `${hours} ч`, `${minutes} мин`].filter(Boolean).join(' '); }
function usedMemory(total, available) { return Math.max(0, Number(total || 0) - Number(available || 0)); }
function usagePercent(used, total) { return total ? `${Math.round(used / total * 100)}% использовано` : 'не используется'; }

async function openInstanceLogs(id) { activeLogService = `instance:${id}`; await openDiagnostics(); }
async function openDiagnostics() { setActiveNavigation($('#openDiagnostics')); if (!$('#diagnosticsDialog').open) $('#diagnosticsDialog').showModal(); await loadDiagnostics(); }
async function loadDiagnostics() {
  const refresh = $('#refreshDiagnostics'); refresh.disabled = true; $('#diagnosticsLoading').hidden = false; $('#diagnosticsLoading').classList.remove('bad'); $('#diagnosticsLoading').textContent = 'Загружаем диагностику…'; $('#diagnosticsContent').hidden = true;
  try {
    diagnosticsData = await api(`/api/diagnostics?service=${encodeURIComponent(activeLogService)}`); const host = diagnosticsData.host; const memoryUsed = usedMemory(host.memoryTotal, host.memoryAvailable), swapUsed = usedMemory(host.swapTotal, host.swapFree);
    $('#diagHostname').textContent = host.hostname || '—'; $('#diagPlatform').textContent = `${host.os}/${host.arch}`; $('#diagUptime').textContent = formatUptime(host.uptimeSeconds);
    $('#diagUpdated').textContent = new Date(diagnosticsData.generatedAt).toLocaleString('ru-RU'); $('#diagLoad').textContent = `${host.load1.toFixed(2)} · ${host.load5.toFixed(2)} · ${host.load15.toFixed(2)}`;
    $('#diagCpus').textContent = `CPU: ${host.cpus}`; $('#diagMemory').textContent = `${formatGiB(memoryUsed)} / ${formatGiB(host.memoryTotal)}`; $('#diagMemoryPercent').textContent = usagePercent(memoryUsed, host.memoryTotal);
    $('#diagSwap').textContent = `${formatGiB(swapUsed)} / ${formatGiB(host.swapTotal)}`; $('#diagSwapPercent').textContent = usagePercent(swapUsed, host.swapTotal);
    $('#diagnosticsWarning').textContent = diagnosticsData.hostError || ''; $('#diagnosticsWarning').hidden = !diagnosticsData.hostError;
    renderLogTabs(); renderServiceLog(); $('#diagnosticsLoading').hidden = true; $('#diagnosticsContent').hidden = false;
  } catch (error) { $('#diagnosticsLoading').textContent = error.message; $('#diagnosticsLoading').classList.add('bad'); }
  finally { refresh.disabled = false; }
}
function renderLogTabs() {
  const services = diagnosticsData?.services || {}, keys = Object.keys(services);
  if (!services[activeLogService]) activeLogService = keys.find(key => key.startsWith('instance:')) || 'webgui';
  const tabs = keys.sort((a, b) => Number(a === 'webgui') - Number(b === 'webgui')).map(key => {
    const button = actionButton(services[key].name, key === activeLogService ? 'active' : '', () => { activeLogService = key; loadDiagnostics(); });
    button.dataset.logService = key; return button;
  });
  $('#logServiceTabs').replaceChildren(...tabs);
}
function renderServiceLog() {
  const journal = diagnosticsData?.services?.[activeLogService]; if (!journal) return;
  $('#logServiceName').textContent = journal.name; $('#logUnit').textContent = journal.unit; $('#logError').textContent = journal.error || ''; $('#logError').hidden = !journal.error; $('#serviceLog').textContent = journal.log || 'Журнал пуст.';
}
$('#openDiagnostics').onclick = openDiagnostics; $('#refreshDiagnostics').onclick = loadDiagnostics; $('#closeDiagnostics').onclick = () => $('#diagnosticsDialog').close();
$('#diagnosticsDialog').addEventListener('cancel', event => { event.preventDefault(); $('#diagnosticsDialog').close(); });

setInterval(() => { if (!$('#panel').hidden && !document.querySelector('dialog[open]')) Promise.all([loadInstances(), loadStatus()]).catch(() => {}); }, 5000);
boot();

