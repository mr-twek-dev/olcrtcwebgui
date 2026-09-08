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
  set('#provider', s.provider);
  set('#providerToken', s.providerToken);
  set('#transport', s.transport);
  set('#roomId', s.roomId);
  set('#roomChannel', s.roomChannel);
  set('#cryptoKey', s.cryptoKey);
  set('#cryptoKeyFile', s.cryptoKeyFile);
  set('#dns', s.dns);
  set('#dataDir', s.dataDir);
  $('#debug').checked = Boolean(s.debug);
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
  refreshForm();
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
    await api('/api/settings', {method: 'PUT', body: JSON.stringify(formSettings())});
    notify('Конфигурация сохранена в olcrtc.yaml');
    await loadStatus();
  } catch (error) {
    notify(error.message, true);
  }
});

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
    $('#topStatus').textContent = s.active ? '● Сервер работает' : '○ Сервер остановлен';
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
    notify(update.available ? 'Доступна новая версия' : 'Установлена актуальная версия');
  } catch (error) {
    notify(error.message, true);
  } finally {
    button.disabled = false;
  }
};

$('#installUpdate').onclick = async () => {
  const button = $('#installUpdate');
  button.disabled = true;
  button.textContent = 'Обновляем...';
  try {
    const result = await api('/api/update/install', {method: 'POST'});
    notify(result.message);
    await loadStatus();
  } catch (error) {
    notify(error.message, true);
  } finally {
    button.textContent = 'Обновить';
    button.disabled = false;
  }
};

document.querySelectorAll('[data-scroll]').forEach(button => {
  button.onclick = () => document.getElementById(button.dataset.scroll).scrollIntoView({behavior: 'smooth'});
});

boot();
