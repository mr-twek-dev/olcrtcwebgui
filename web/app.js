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
    const current = formSettings();
    const payload = {
      ...current,
      profiles,
      failover: {retryDelay: text('#failoverRetryDelay'), maxCycles: number('#failoverMaxCycles')}
    };
    await api('/api/settings', {method: 'PUT', body: JSON.stringify(payload)});
    notify(`Сохранено профилей: ${profiles.length}. Порядок используется для failover.`);
    await loadStatus();
  } catch (error) {
    notify(error.message, true);
  }
});

function renderProfiles() {
  const select = $('#profileSelect');
  select.replaceChildren(...profiles.map((profile, index) => {
    const option = document.createElement('option');
    option.value = String(index);
    option.textContent = profile.name;
    return option;
  }));
  select.value = String(activeProfile);
  set('#profileName', profiles[activeProfile]?.name || '');
  $('#deleteProfile').disabled = profiles.length === 1;
  $('#moveProfileUp').disabled = activeProfile === 0;
  $('#moveProfileDown').disabled = activeProfile === profiles.length - 1;
  set('#shareComment', profiles[activeProfile]?.name || '');
  clearShare();
}

function saveActiveProfile() {
  if (!profiles[activeProfile]) return;
  const name = text('#profileName') || `Профиль ${activeProfile + 1}`;
  profiles[activeProfile] = {name, ...connectionSettings(formSettings())};
}

$('#profileSelect').addEventListener('change', event => {
  saveActiveProfile();
  activeProfile = Number(event.target.value);
  renderProfiles();
  fillConnectionSettings(profiles[activeProfile]);
  refreshForm();
});

$('#profileName').addEventListener('input', () => {
  if (!profiles[activeProfile]) return;
  profiles[activeProfile].name = text('#profileName') || `Профиль ${activeProfile + 1}`;
  $('#profileSelect').options[activeProfile].textContent = profiles[activeProfile].name;
});

$('#addProfile').onclick = () => {
  saveActiveProfile();
  const defaults = profiles[activeProfile] ? structuredClone(profiles[activeProfile]) : connectionSettings(formSettings());
  profiles.push({...defaults, name: `Профиль ${profiles.length + 1}`});
  activeProfile = profiles.length - 1;
  renderProfiles();
  fillConnectionSettings(profiles[activeProfile]);
  refreshForm();
};

$('#duplicateProfile').onclick = () => {
  saveActiveProfile();
  const copy = structuredClone(profiles[activeProfile]);
  copy.name = `${copy.name} — копия`;
  profiles.splice(activeProfile + 1, 0, copy);
  activeProfile += 1;
  renderProfiles();
  fillConnectionSettings(copy);
  refreshForm();
};

function moveActiveProfile(offset) {
  saveActiveProfile();
  const target = activeProfile + offset;
  if (target < 0 || target >= profiles.length) return;
  [profiles[activeProfile], profiles[target]] = [profiles[target], profiles[activeProfile]];
  activeProfile = target;
  renderProfiles();
  fillConnectionSettings(profiles[activeProfile]);
  refreshForm();
}

$('#moveProfileUp').onclick = () => moveActiveProfile(-1);
$('#moveProfileDown').onclick = () => moveActiveProfile(1);

$('#deleteProfile').onclick = () => {
  if (profiles.length === 1 || !confirm(`Удалить профиль «${profiles[activeProfile].name}»?`)) return;
  profiles.splice(activeProfile, 1);
  activeProfile = Math.min(activeProfile, profiles.length - 1);
  renderProfiles();
  fillConnectionSettings(profiles[activeProfile]);
  refreshForm();
};

function clearShare() {
  set('#clientUri', '');
  $('#qrCode').hidden = true;
  $('#qrCode').removeAttribute('src');
}

$('#generateShare').onclick = async () => {
  const button = $('#generateShare');
  button.disabled = true;
  try {
    saveActiveProfile();
    const settings = {...formSettings(), ...connectionSettings(profiles[activeProfile])};
    const result = await api('/api/share', {method: 'POST', body: JSON.stringify({settings, comment: text('#shareComment')})});
    set('#clientUri', result.uri);
    $('#qrCode').src = result.qr;
    $('#qrCode').hidden = false;
    notify('Клиентская ссылка и QR-код созданы локально.');
  } catch (error) {
    notify(error.message, true);
  } finally {
    button.disabled = false;
  }
};

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
  button.onclick = () => document.getElementById(button.dataset.scroll).scrollIntoView({behavior: 'smooth'});
});

boot();

