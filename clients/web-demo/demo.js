// yunmao 云养猫 · 开发回归 Demo
//
// 流程：
// 1. 登录 user-svc 拿 login JWT；
// 2. 拿房间订阅 token（room-svc 签发）；
// 3. 连 WebSocket，发送 Auth 帧 + Subscribe 帧；
// 4. flv.js 播放 media-edge HTTP-FLV；
// 5. 触发 feeding-svc 投喂，观察 WS 事件流。

const $ = (id) => document.getElementById(id);
const logBox = $('log');

function log(line, cls = 'info') {
  const ts = new Date().toISOString().substring(11, 23);
  const el = document.createElement('div');
  el.className = cls;
  el.textContent = `[${ts}] ${line}`;
  logBox.appendChild(el);
  logBox.scrollTop = logBox.scrollHeight;
}

let state = {
  loginJwt: '',
  roomToken: '',
  userId: '',
  ws: null,
  flvPlayer: null,
  hlsPlayer: null,
};

async function postJson(url, body, headers = {}) {
  const r = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...headers },
    body: JSON.stringify(body || {}),
  });
  if (!r.ok) throw new Error(`${r.status} ${await r.text()}`);
  return r.json();
}

$('btnLogin').onclick = async () => {
  try {
    const phone = $('loginPhone').value || `+8613${Math.floor(Math.random() * 1e9).toString().padStart(9, '0')}`;
    const data = await postJson(`${$('userSvc').value}/v1/auth/login`, { phone_e164: phone });
    state.loginJwt = data.access_token;
    state.userId = (data.user && data.user.id) || '';
    $('loginJwt').textContent = state.loginJwt;
    log(`登录成功 user=${state.userId}`, 'ok');
  } catch (e) {
    log(`登录失败：${e.message}`, 'err');
  }
};

$('btnSubToken').onclick = async () => {
  try {
    if (!state.loginJwt) throw new Error('请先登录');
    const room = $('roomId').value;
    const data = await postJson(
      `${$('roomSvc').value}/v1/rooms/${room}/subscriptions`,
      {},
      { Authorization: `Bearer ${state.loginJwt}` }
    );
    state.roomToken = data.token;
    $('roomToken').textContent = state.roomToken;
    log(`房间订阅 token 已签发 (ttl=${data.ttl_sec || '?'}s)`, 'ok');
  } catch (e) {
    log(`订阅 token 失败：${e.message}`, 'err');
  }
};

$('btnConnectWs').onclick = () => {
  if (state.ws) { try { state.ws.close(); } catch (_) {} }
  const ws = new WebSocket($('wsUrl').value);
  state.ws = ws;
  ws.onopen = () => {
    log('WS 已连接', 'ok');
    // 优先用 roomToken；没有就退化到 loginJwt（游客）
    const token = state.roomToken || state.loginJwt;
    if (token) {
      ws.send(JSON.stringify({ op: 'auth', token }));
      log('已发送 Auth 帧');
    }
  };
  ws.onmessage = (m) => {
    let f;
    try { f = JSON.parse(m.data); } catch { log(`ws raw: ${m.data}`); return; }
    if (f.op === 'event' || f.type) {
      const t = f.type || f.event || '';
      if (t === 'room.chat.message' || t === 'chat.message') {
        const d = f.data || f.payload || {};
        renderChatMessage(d);
        return;
      }
      if (t === 'room.chat.moderation' || t === 'chat.moderation') {
        const d = f.data || f.payload || {};
        applyChatModeration(d);
        return;
      }
    }
    log(`ws ${f.op || '?'}: ${JSON.stringify(f)}`, f.op === 'error' ? 'err' : 'info');
  };
  ws.onclose = () => log('WS 断开', 'warn');
  ws.onerror = (e) => log(`WS 错误: ${e.message || e}`, 'err');
};

$('btnSubscribe').onclick = () => {
  if (!state.ws) return log('请先连 WS', 'warn');
  state.ws.send(JSON.stringify({ op: 'subscribe', rooms: [$('roomId').value] }));
  log(`已发送 Subscribe → ${$('roomId').value}`);
};

function destroyPlayers() {
  if (state.flvPlayer) { try { state.flvPlayer.destroy(); } catch (_) {} state.flvPlayer = null; }
  if (state.hlsPlayer) { try { state.hlsPlayer.destroy(); } catch (_) {} state.hlsPlayer = null; }
}

$('btnPlay').onclick = () => {
  destroyPlayers();
  const proto = $('playProto').value;
  const v = $('player');
  const tsStart = performance.now();
  v.addEventListener('playing', () => {
    const took = ((performance.now() - tsStart) / 1000).toFixed(2);
    log(`播放就绪 (${proto}) start_latency=${took}s`, 'ok');
  }, { once: true });

  if (proto === 'll-hls') {
    if (!window.Hls || !Hls.isSupported()) {
      // Safari 原生支持
      v.src = $('hlsUrl').value;
      v.play().catch((e) => log(`HLS native 播放失败: ${e.message}`, 'err'));
      log(`Safari 原生拉 LL-HLS: ${$('hlsUrl').value}`);
      return;
    }
    const hls = new Hls({
      lowLatencyMode: true,
      backBufferLength: 4,
      liveSyncDuration: 1.5,
      liveMaxLatencyDuration: 5,
    });
    hls.loadSource($('hlsUrl').value);
    hls.attachMedia(v);
    hls.on(Hls.Events.ERROR, (_, data) => {
      log(`HLS 错误 type=${data.type} fatal=${data.fatal} details=${data.details}`,
          data.fatal ? 'err' : 'warn');
    });
    state.hlsPlayer = hls;
    log(`正在拉 LL-HLS: ${$('hlsUrl').value}`);
    return;
  }

  if (!window.flvjs || !flvjs.isSupported()) return log('flv.js 不可用', 'err');
  const player = flvjs.createPlayer({ type: 'flv', isLive: true, url: $('flvUrl').value });
  player.attachMediaElement(v);
  player.load();
  player.play().catch((e) => log(`播放失败: ${e.message}`, 'err'));
  state.flvPlayer = player;
  log(`正在拉流: ${$('flvUrl').value}`);
};

$('btnWhep').onclick = async () => {
  destroyPlayers();
  const v = $('player');
  const tsStart = performance.now();
  v.addEventListener('playing', () => {
    const took = ((performance.now() - tsStart) / 1000).toFixed(2);
    log(`WHEP 播放就绪 start_latency=${took}s`, 'ok');
  }, { once: true });

  const url = $('whepUrl').value;
  try {
    const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
    pc.addTransceiver('video', { direction: 'recvonly' });
    pc.addTransceiver('audio', { direction: 'recvonly' });
    pc.ontrack = (ev) => {
      log(`WHEP 收到 track kind=${ev.track.kind}`, 'ok');
      if (!v.srcObject) v.srcObject = new MediaStream();
      v.srcObject.addTrack(ev.track);
    };
    const offer = await pc.createOffer({ offerToReceiveAudio: true, offerToReceiveVideo: true });
    await pc.setLocalDescription(offer);
    log(`POST WHEP ${url}`);
    const r = await fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/sdp',
        ...(state.roomToken ? { Authorization: `Bearer ${state.roomToken}` } : {}),
      },
      body: offer.sdp,
    });
    if (!r.ok) throw new Error(`${r.status} ${await r.text()}`);
    const answerSdp = await r.text();
    await pc.setRemoteDescription({ type: 'answer', sdp: answerSdp });
    log(`WHEP 信令完成 location=${r.headers.get('location') || '-'}`, 'ok');
    state.whepPc = pc;
  } catch (e) {
    log(`WHEP 启动失败: ${e.message}`, 'err');
  }
};

$('btnFeed').onclick = async () => {
  try {
    if (!state.loginJwt) throw new Error('需要登录');
    const clientReqId = `cli_${Date.now()}_${Math.floor(Math.random() * 1e6)}`;
    const body = {
      room_id: $('roomId').value,
      cat_id: 'cat_demo',
      device_id: 'dev_demo',
      amount_grams: 5,
      client_request_id: clientReqId,
    };
    const data = await postJson(
      `${$('feedingSvc').value}/v1/feed-requests`,
      body,
      { Authorization: `Bearer ${state.loginJwt}` }
    );
    log(`feeding 请求已提交 id=${data.feed_request_id || data.id || '?'} status=${data.status || '?'}`, 'ok');
  } catch (e) {
    log(`投喂失败: ${e.message}`, 'err');
  }
};

$('btnMeta').onclick = async () => {
  try {
    const flvUrl = new URL($('flvUrl').value);
    const room = $('roomId').value;
    const metaUrl = `${flvUrl.origin}/live/${room}/meta.json`;
    const r = await fetch(metaUrl);
    if (!r.ok) throw new Error(`${r.status} ${await r.text()}`);
    const m = await r.json();
    const res = m.video ? `${m.video.width}x${m.video.height}@${m.video.fps}fps` : '-';
    const audio = m.audio_present ? `${m.audio.codec}/${m.audio.sample_rate}/${m.audio.channels}ch` : 'none';
    const br = m.source_bitrate_bps ? `${(m.source_bitrate_bps / 1000).toFixed(0)}kbps` : '-';
    const abr = (m.abr_active_ladder || []).join(',') || 'src';
    $('streamMeta').textContent = `res=${res} audio=${audio} src_br=${br} abr=${abr}`;
    log(`流元数据已刷新: res=${res} audio=${audio}`, 'ok');
  } catch (e) {
    log(`拉流元数据失败：${e.message}`, 'err');
  }
};

// ---- 弹幕（第六轮 F；第七轮：增加 message-id + recall 处理） ----
function chatPush(line, who = '', id = '') {
  const box = $('chatScroll');
  if (!box) return;
  const row = document.createElement('div');
  row.className = 'chat-row';
  if (id) row.dataset.msgId = id;
  row.textContent = (who ? `[${who}] ` : '') + line;
  box.appendChild(row);
  box.scrollTop = box.scrollHeight;
  while (box.childNodes.length > 80) box.removeChild(box.firstChild);
}

function renderChatMessage(d) {
  const id = d.id || '';
  chatPush(d.body || '', d.user_id || '', id);
}

// 第七轮：审核动作（recall/hide/warn/mute/block/delete）下发到客户端。
function applyChatModeration(d) {
  const id = d.id || d.message_id || '';
  const action = (d.action || d.status || '').toLowerCase();
  if (!id || !action) return;
  const box = $('chatScroll');
  if (!box) return;
  const row = box.querySelector(`[data-msg-id="${CSS.escape(id)}"]`);
  if (!row) {
    log(`chat.moderation: 未找到 id=${id} action=${action}`, 'warn');
    return;
  }
  switch (action) {
    case 'recall':
    case 'delete':
      row.remove();
      log(`弹幕已撤回 id=${id}`, 'warn');
      break;
    case 'hide':
      row.textContent = '[已隐藏]';
      row.classList.add('hidden');
      break;
    case 'warn':
      row.classList.add('warned');
      row.textContent += ' ⚠ ' + (d.reason || 'flagged');
      break;
    case 'mute':
      log(`用户 ${row.textContent.split(']')[0]} 被禁言`, 'warn');
      break;
    default:
      log(`chat.moderation 未知 action=${action}`, 'warn');
  }
}

async function sendChat() {
  const text = $('chatInput').value.trim();
  if (!text) return;
  try {
    const room = $('roomId').value;
    const url = `${$('chatSvc').value}/api/v1/rooms/${room}/chat`;
    const headers = { 'Content-Type': 'application/json' };
    if (state.userId) headers['X-User-Id'] = state.userId;
    const r = await fetch(url, {
      method: 'POST',
      headers,
      body: JSON.stringify({ user_id: state.userId, room_id: room, body: text }),
    });
    if (!r.ok) throw new Error(`${r.status} ${await r.text()}`);
    chatPush(text, state.userId || 'me');
    $('chatInput').value = '';
  } catch (e) {
    log(`弹幕发送失败: ${e.message}`, 'err');
  }
}

const chatBtn = $('btnSendChat');
if (chatBtn) chatBtn.onclick = sendChat;
const chatInputEl = $('chatInput');
if (chatInputEl) {
  chatInputEl.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') sendChat();
  });
}

log('就绪：依次点击 登录 → 订阅 token → 连 WS → 订阅房间 → 播放 → 投喂；弹幕请填好 chat-svc 地址');
