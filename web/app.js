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
  setTimeout(() => {
    notice.textContent = '';
    notice.classList.remove('bad');
  }, 6000);
};

const text = id => $(id).value.trim();
const number = id => Number($(id).value || 0);
const set = (id, value) => { $(id).value = value ?? ''; };
const formatGiB = bytes => bytes ? `${(bytes / (1024 ** 3)).toFixed(1)} ГБ` : '0 ГБ';
let profiles = [];
let activeProfile = 0;
let pendingNewProfile = -1;
let diagnosticsData = null;
let activeLogService = 'olcrtc';

async function boot() {
  try {
    const me = await api('/api/me');
    $('#login').hidden = true;
    $('#panel').hidden = false;
    $('#currentUser').textContent = me.username;
    await Promise.all([loadSettings(), loadStatus()]);
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
  } catch (error) {
    $('#loginError').textContent = error.message;
  }
});

$('#logout').onclick = async () => {
  await api('/api/logout', {method: 'POST'});
  location.reload();
};

async function loadSettings() {
  const s = await api('/api/settings');
  set('#mode', s.mode);
  set('#dataDir', s.dataDir);
  $('#debug').checked = Boolean(s.debug);
  set('#failoverRetryDelay', s.failover?.retryDelay || '2s');
  set('#failoverMaxCycles', s.failover?.maxCycles || 0);
  profiles = (s.profiles?.length ? s.profiles : [{name: 'Основной', ...connectionSettings(s)}]).map(profile => structuredClone(profile));
  activeProfile = 0;
  renderProfiles();
  fillConnectionSettings(profiles[0]);
  refreshForm();
}

function connectionSettings(s) {
  const {mode, dataDir, debug, profiles: ignoredProfiles, failover, name, ...connection} = s;
  return connection;
}

function fillConnectionSettings(s) {
  set('#provider', s.provider);
  set('#providerToken', s.providerToken);
  set('#transport', s.transport);
  set('#roomId', s.roomId);
  set('#roomChannel', s.roomChannel);
  set('#cryptoKey', s.cryptoKey);
  set('#cryptoKeyFile', s.cryptoKeyFile);
  set('#dns', s.dns);
  set('#engineName', s.engine?.name);
  set('#engineUrl', s.engine?.url);
  set('#engineToken', s.engine?.token);
  set('#socksHost', s.socks?.host);
  set('#socksPort', s.socks?.port);
  set('#socksUser', s.socks?.user);
  set('#socksPass', s.socks?.pass);
  set('#proxyAddr', s.socks?.proxyAddr);
  set('#proxyPort', s.socks?.proxyPort);
  set('#proxyUser', s.socks?.proxyUser);
  set('#proxyPass', s.socks?.proxyPass);
  set('#videoCodec', s.video?.codec);
  set('#videoWidth', s.video?.width);
  set('#videoHeight', s.video?.height);
  set('#videoFps', s.video?.fps);
  set('#videoQrSize', s.video?.qrSize);
  set('#videoQrRecovery', s.video?.qrRecovery);
  set('#videoTileModule', s.video?.tileModule);
  set('#videoTileRs', s.video?.tileRs);
  set('#vp8Fps', s.vp8?.fps);
  set('#vp8BatchSize', s.vp8?.batchSize);
  set('#seiFps', s.sei?.fps);
  set('#seiBatchSize', s.sei?.batchSize);
  set('#seiFragmentSize', s.sei?.fragmentSize);
  set('#seiAckTimeoutMs', s.sei?.ackTimeoutMs);
  set('#livenessInterval', s.liveness?.interval);
  set('#livenessTimeout', s.liveness?.timeout);
  set('#livenessFailures', s.liveness?.failures);
  set('#maxSessionDuration', s.lifecycle?.maxSessionDuration);
  set('#maxPayloadSize', s.traffic?.maxPayloadSize);
  set('#minDelay', s.traffic?.minDelay);
  set('#maxDelay', s.traffic?.maxDelay);
}

function formSettings() {
  return {
    mode: text('#mode'),
    provider: text('#provider'),
    providerToken: $('#providerToken').value.trim(),
    transport: text('#transport'),
    roomId: text('#roomId'),
    roomChannel: text('#roomChannel'),
    cryptoKey: text('#cryptoKey'),
    cryptoKeyFile: text('#cryptoKeyFile'),
    dns: text('#dns'),
    dataDir: text('#dataDir'),
    debug: $('#debug').checked,
    engine: {name: text('#engineName'), url: text('#engineUrl'), token: $('#engineToken').value.trim()},
    socks: {
      host: text('#socksHost'), port: number('#socksPort'), user: text('#socksUser'), pass: $('#socksPass').value,
      proxyAddr: text('#proxyAddr'), proxyPort: number('#proxyPort'), proxyUser: text('#proxyUser'), proxyPass: $('#proxyPass').value
    },
    video: {
      codec: text('#videoCodec'), width: number('#videoWidth'), height: number('#videoHeight'), fps: number('#videoFps'),
      qrSize: number('#videoQrSize'), qrRecovery: text('#videoQrRecovery'), tileModule: number('#videoTileModule'), tileRs: number('#videoTileRs')
    },
    vp8: {fps: number('#vp8Fps'), batchSize: number('#vp8BatchSize')},
    sei: {fps: number('#seiFps'), batchSize: number('#seiBatchSize'), fragmentSize: number('#seiFragmentSize'), ackTimeoutMs: number('#seiAckTimeoutMs')},
    liveness: {interval: text('#livenessInterval'), timeout: text('#livenessTimeout'), failures: number('#livenessFailures')},
    lifecycle: {maxSessionDuration: text('#maxSessionDuration')},
    traffic: {maxPayloadSize: number('#maxPayloadSize'), minDelay: text('#minDelay'), maxDelay: text('#maxDelay')}
  };
}

$('#settingsForm').addEventListener('submit', async event => {
  event.preventDefault();
  try {
    saveActiveProfile();
    await persistProfiles(`Профиль «${profiles[activeProfile].name}» сохранён.`);
    pendingNewProfile = -1;
    $('#profileSettingsDialog').close();
    renderProfiles();
  } catch (error) {
    notify(error.message, true);
  }
});

async function persistProfiles(message) {
  const current = formSettings();
  const payload = {
    ...current,
    profiles,
    failover: {retryDelay: text('#failoverRetryDelay'), maxCycles: number('#failoverMaxCycles')}
  };
  await api('/api/settings', {method: 'PUT', body: JSON.stringify(payload)});
  if (message) notify(message);
  await loadStatus();
}

function renderProfiles() {
  const list = $('#profileList');
  list.replaceChildren(...profiles.map((profile, index) => {
    const card = document.createElement('section');
    card.className = 'profile-card';

    const order = document.createElement('span');
    order.className = 'profile-order';
    order.textContent = String(index + 1);

    const summary = document.createElement('div');
    summary.className = 'profile-summary';
    const name = document.createElement('h3');
    name.textContent = profile.name;
    const meta = document.createElement('p');
    meta.textContent = `${providerLabel(profile.provider)} · ${transportLabel(profile.transport)}`;
    const room = document.createElement('small');
    room.textContent = profile.roomId || 'Комната не указана';
    summary.append(name, meta, room);

    const actions = document.createElement('div');
    actions.className = 'profile-card-actions';
    actions.append(
      actionButton('Конфигурация', 'secondary', () => openProfileSettings(index)),
      actionButton('Подключение клиента', '', () => openClientConnection(index)),
      actionButton('↑', 'icon secondary', () => moveProfile(index, -1), 'Выше в порядке failover', index === 0),
      actionButton('↓', 'icon secondary', () => moveProfile(index, 1), 'Ниже в порядке failover', index === profiles.length - 1),
      actionButton('Дублировать', 'text secondary', () => duplicateProfile(index)),
      actionButton('Удалить', 'text danger', () => deleteProfile(index), '', profiles.length === 1)
    );
    card.append(order, summary, actions);
    return card;
  }));
}

function actionButton(label, className, handler, title = '', disabled = false) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = className;
  button.textContent = label;
  button.title = title;
  button.disabled = disabled;
  button.onclick = handler;
  return button;
}

function providerLabel(provider) {
  return {jitsi: 'Jitsi', telemost: 'Яндекс Телемост', wbstream: 'WB Stream', none: 'Прямое подключение'}[provider] || provider;
}

function transportLabel(transport) {
  return {datachannel: 'Data channel', vp8channel: 'VP8 channel', seichannel: 'SEI channel', videochannel: 'Video channel'}[transport] || transport;
}

function saveActiveProfile() {
  if (!profiles[activeProfile]) return;
  const name = text('#profileName') || `Профиль ${activeProfile + 1}`;
  profiles[activeProfile] = {name, ...connectionSettings(formSettings())};
}

function openProfileSettings(index, isNew = false) {
  activeProfile = index;
  pendingNewProfile = isNew ? index : -1;
  set('#profileName', profiles[index].name);
  fillConnectionSettings(profiles[index]);
  $('#settingsDialogTitle').textContent = profiles[index].name;
  refreshForm();
  $('#profileSettingsDialog').showModal();
}

function closeProfileSettings() {
  if (pendingNewProfile >= 0) {
    profiles.splice(pendingNewProfile, 1);
    activeProfile = Math.max(0, Math.min(activeProfile, profiles.length - 1));
    pendingNewProfile = -1;
    renderProfiles();
  }
  $('#profileSettingsDialog').close();
  loadSettings().catch(error => notify(error.message, true));
}

$('#closeSettings').onclick = closeProfileSettings;
$('#cancelSettings').onclick = closeProfileSettings;
$('#profileSettingsDialog').addEventListener('cancel', event => {
  event.preventDefault();
  closeProfileSettings();
});

$('#addProfile').onclick = () => {
  const source = profiles[activeProfile] || connectionSettings(formSettings());
  profiles.push({...structuredClone(source), name: uniqueProfileName('Новый профиль')});
  activeProfile = profiles.length - 1;
  renderProfiles();
  openProfileSettings(activeProfile, true);
};

function uniqueProfileName(base) {
  let candidate = base;
  let suffix = 2;
  while (profiles.some(profile => profile.name === candidate)) candidate = `${base} ${suffix++}`;
  return candidate;
}

function duplicateProfile(index) {
  const copy = structuredClone(profiles[index]);
  copy.name = uniqueProfileName(`${copy.name} — копия`);
  profiles.splice(index + 1, 0, copy);
  activeProfile = index + 1;
  renderProfiles();
  openProfileSettings(activeProfile, true);
}

async function moveProfile(index, offset) {
  const target = index + offset;
  if (target < 0 || target >= profiles.length) return;
  [profiles[index], profiles[target]] = [profiles[target], profiles[index]];
  activeProfile = target;
  renderProfiles();
  try {
    await persistProfiles('Порядок failover сохранён.');
  } catch (error) {
    notify(error.message, true);
    await loadSettings();
  }
}

async function deleteProfile(index) {
  if (profiles.length === 1 || !confirm(`Удалить профиль «${profiles[index].name}»?`)) return;
  profiles.splice(index, 1);
  activeProfile = Math.min(activeProfile, profiles.length - 1);
  renderProfiles();
  try {
    await persistProfiles('Профиль удалён.');
  } catch (error) {
    notify(error.message, true);
    await loadSettings();
  }
}

function clearShare() {
  set('#clientUri', '');
  $('#qrCode').removeAttribute('src');
  $('#shareResult').hidden = true;
  $('#shareLoading').hidden = false;
}

async function openClientConnection(index) {
  activeProfile = index;
  set('#shareComment', profiles[index].name);
  $('#clientDialogTitle').textContent = profiles[index].name;
  clearShare();
  $('#clientDialog').showModal();
  await generateShare();
}

async function generateShare() {
  const button = $('#regenerateShare');
  button.disabled = true;
  $('#shareLoading').hidden = false;
  $('#shareLoading').textContent = 'Создаём ссылку и QR-код…';
  $('#shareLoading').classList.remove('bad');
  $('#shareResult').hidden = true;
  try {
    const settings = {...formSettings(), ...connectionSettings(profiles[activeProfile])};
    const result = await api('/api/share', {method: 'POST', body: JSON.stringify({settings, comment: text('#shareComment')})});
    set('#clientUri', result.uri);
    $('#qrCode').src = result.qr;
    $('#shareLoading').hidden = true;
    $('#shareResult').hidden = false;
  } catch (error) {
    $('#shareLoading').textContent = error.message;
    $('#shareLoading').classList.add('bad');
  } finally {
    button.disabled = false;
  }
}

$('#regenerateShare').onclick = generateShare;
$('#closeClient').onclick = () => $('#clientDialog').close();
$('#clientDialog').addEventListener('close', () => $('#shareLoading').classList.remove('bad'));

$('#copyClientUri').onclick = async () => {
  const uri = text('#clientUri');
  if (!uri) return notify('Сначала создайте клиентскую ссылку.', true);
  try {
    await navigator.clipboard.writeText(uri);
    notify('Клиентская ссылка скопирована.');
  } catch {
    notify('Не удалось скопировать автоматически. Выделите ссылку вручную.', true);
  }
};

function refreshForm() {
  const mode = text('#mode');
  const provider = text('#provider');
  const transport = text('#transport');
  $('#clientSocksGroup').hidden = mode !== 'cnc';
  $('#serverSocksGroup').hidden = mode !== 'srv';
  $('#engineGroup').hidden = provider !== 'none';
  document.querySelector('.provider-token').hidden = provider !== 'wbstream';
  $('#vp8Group').hidden = transport !== 'vp8channel';
  $('#seiGroup').hidden = transport !== 'seichannel';
  $('#videoGroup').hidden = transport !== 'videochannel';

  for (const option of $('#transport').options) {
    option.disabled = provider === 'telemost' && (option.value === 'datachannel' || option.value === 'seichannel');
  }
  if (provider === 'telemost' && (transport === 'datachannel' || transport === 'seichannel')) {
    $('#transport').value = 'vp8channel';
    return refreshForm();
  }

  const hints = {
    'jitsi:datachannel': 'Рекомендуемая и самая быстрая комбинация. Для Jitsi укажите полный URL комнаты.',
    'wbstream:datachannel': 'Нужен токен аккаунта или модератора с canPublishData=true на обеих сторонах.',
    'telemost:vp8channel': 'Стабильная комбинация для Телемоста.',
    'jitsi:seichannel': 'Работает, но на загруженных Jitsi datachannel или vp8channel надёжнее.',
    'telemost:videochannel': 'Работает медленно; используйте vp8channel, если возможно.'
  };
  $('#combinationHint').textContent = hints[`${provider}:${transport}`] || 'Параметры провайдера и транспорта должны совпадать на сервере и клиенте.';
}

$('#mode').addEventListener('change', refreshForm);
$('#provider').addEventListener('change', refreshForm);
$('#transport').addEventListener('change', refreshForm);
$('#videoCodec').addEventListener('change', () => {
  if (text('#videoCodec') === 'tile') {
    set('#videoWidth', 1080);
    set('#videoHeight', 1080);
  }
});
$('#cryptoKey').addEventListener('input', () => { if (text('#cryptoKey')) set('#cryptoKeyFile', ''); });
$('#cryptoKeyFile').addEventListener('input', () => { if (text('#cryptoKeyFile')) set('#cryptoKey', ''); });
$('#generateKey').onclick = () => {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  set('#cryptoKey', Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join(''));
  set('#cryptoKeyFile', '');
	notify('Новый ключ создан. Скопируйте его на вторую сторону туннеля.');
};
$('#copyKey').onclick = async () => {
	const key = text('#cryptoKey');
	if (!key) {
		notify('Сначала создайте или введите ключ.', true);
		return;
	}
	try {
		await navigator.clipboard.writeText(key);
		notify('Ключ скопирован.');
	} catch {
		notify('Не удалось скопировать автоматически. Выделите ключ вручную.', true);
	}
};

async function loadStatus() {
  try {
    const s = await api('/api/status');
    $('#statusText').textContent = s.status;
    $('#version').textContent = s.version || '-';
    $('#currentVersion').textContent = s.version || '-';
    $('#projectDir').textContent = s.projectDir;
    $('#configPath').textContent = s.configPath;
    $('#memoryTotal').textContent = formatGiB(s.memoryTotal);
    $('#swapTotal').textContent = formatGiB(s.swapTotal);
    $('#memoryHint').hidden = !s.swapRecommended;
    $('#topStatus').textContent = s.active ? '● Сервер работает' : (s.installed ? '○ Сервер остановлен' : '○ Требуется установка');
    $('#topStatus').classList.toggle('inactive', !s.active);
  } catch (error) {
    notify(error.message, true);
  }
}

document.querySelectorAll('[data-action]').forEach(button => {
  button.onclick = async () => {
    button.disabled = true;
    try {
      await api(`/api/service/${button.dataset.action}`, {method: 'POST'});
      notify('Команда выполнена');
      await loadStatus();
    } catch (error) {
      notify(error.message, true);
    } finally {
      button.disabled = false;
    }
  };
});

$('#checkUpdate').onclick = async () => {
  const button = $('#checkUpdate');
  button.disabled = true;
  try {
    const update = await api('/api/update/check', {method: 'POST'});
    $('#currentVersion').textContent = update.current || 'не установлена';
    $('#latestVersion').textContent = update.latest;
    $('#installUpdate').disabled = !update.available;
    notify(update.needsInstall ? 'Требуется сборка и установка сервиса' : (update.available ? 'Доступна новая версия' : 'Установлена актуальная версия'));
  } catch (error) {
    notify(error.message, true);
  } finally {
    button.disabled = false;
  }
};

$('#installUpdate').onclick = async () => {
  const button = $('#installUpdate');
  button.disabled = true;
  button.textContent = 'Устанавливаем...';
  try {
    const result = await api('/api/update/install', {method: 'POST'});
    notify(result.message);
    await loadStatus();
  } catch (error) {
    notify(error.message, true);
  } finally {
    button.textContent = 'Установить / обновить';
    button.disabled = false;
  }
};

document.querySelectorAll('[data-scroll]').forEach(button => {
  button.onclick = () => {
    setActiveNavigation(button);
    document.getElementById(button.dataset.scroll).scrollIntoView({behavior: 'smooth', block: 'start'});
  };
});

function setActiveNavigation(active) {
  document.querySelectorAll('nav button').forEach(button => button.classList.toggle('active', button === active));
}

function formatUptime(seconds) {
  if (!seconds) return '—';
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return [days && `${days} д`, hours && `${hours} ч`, `${minutes} мин`].filter(Boolean).join(' ');
}

function usedMemory(total, available) {
  return Math.max(0, Number(total || 0) - Number(available || 0));
}

function usagePercent(used, total) {
  return total ? `${Math.round(used / total * 100)}% использовано` : 'не используется';
}

async function openDiagnostics() {
  setActiveNavigation($('#openDiagnostics'));
  if (!$('#diagnosticsDialog').open) $('#diagnosticsDialog').showModal();
  await loadDiagnostics();
}

async function loadDiagnostics() {
  const refresh = $('#refreshDiagnostics');
  refresh.disabled = true;
  $('#diagnosticsLoading').hidden = false;
  $('#diagnosticsLoading').classList.remove('bad');
  $('#diagnosticsLoading').textContent = 'Загружаем диагностику…';
  $('#diagnosticsContent').hidden = true;
  try {
    diagnosticsData = await api('/api/diagnostics');
    const host = diagnosticsData.host;
    const memoryUsed = usedMemory(host.memoryTotal, host.memoryAvailable);
    const swapUsed = usedMemory(host.swapTotal, host.swapFree);
    $('#diagHostname').textContent = host.hostname || '—';
    $('#diagPlatform').textContent = `${host.os}/${host.arch}`;
    $('#diagUptime').textContent = formatUptime(host.uptimeSeconds);
    $('#diagUpdated').textContent = new Date(diagnosticsData.generatedAt).toLocaleString('ru-RU');
    $('#diagLoad').textContent = `${host.load1.toFixed(2)} · ${host.load5.toFixed(2)} · ${host.load15.toFixed(2)}`;
    $('#diagCpus').textContent = `CPU: ${host.cpus}`;
    $('#diagMemory').textContent = `${formatGiB(memoryUsed)} / ${formatGiB(host.memoryTotal)}`;
    $('#diagMemoryPercent').textContent = usagePercent(memoryUsed, host.memoryTotal);
    $('#diagSwap').textContent = `${formatGiB(swapUsed)} / ${formatGiB(host.swapTotal)}`;
    $('#diagSwapPercent').textContent = usagePercent(swapUsed, host.swapTotal);
    $('#diagnosticsWarning').textContent = diagnosticsData.hostError || '';
    $('#diagnosticsWarning').hidden = !diagnosticsData.hostError;
    renderServiceLog();
    $('#diagnosticsLoading').hidden = true;
    $('#diagnosticsContent').hidden = false;
  } catch (error) {
    $('#diagnosticsLoading').textContent = error.message;
    $('#diagnosticsLoading').classList.add('bad');
  } finally {
    refresh.disabled = false;
  }
}

function renderServiceLog() {
  const journal = diagnosticsData?.services?.[activeLogService];
  if (!journal) return;
  document.querySelectorAll('[data-log-service]').forEach(button => button.classList.toggle('active', button.dataset.logService === activeLogService));
  $('#logServiceName').textContent = journal.name;
  $('#logUnit').textContent = journal.unit;
  $('#logError').textContent = journal.error || '';
  $('#logError').hidden = !journal.error;
  $('#serviceLog').textContent = journal.log || 'Журнал пуст.';
}

$('#openDiagnostics').onclick = openDiagnostics;
$('#refreshDiagnostics').onclick = loadDiagnostics;
$('#closeDiagnostics').onclick = () => $('#diagnosticsDialog').close();
$('#diagnosticsDialog').addEventListener('cancel', event => {
  event.preventDefault();
  $('#diagnosticsDialog').close();
});
document.querySelectorAll('[data-log-service]').forEach(button => {
  button.onclick = () => {
    activeLogService = button.dataset.logService;
    renderServiceLog();
  };
});

boot();

