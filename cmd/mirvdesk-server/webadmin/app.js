'use strict';
(function () {
  var token = sessionStorage.getItem('mirvdesk_admin_token') || '';
  var account = null, view = 'overview', state = { users: [], groups: [], devices: [], audit: [], health: null, version: null };
  var titles = {
    overview: ['Обзор', 'Состояние системы и активность'],
    users: ['Пользователи', 'Управление учётными записями'],
    devices: ['Устройства', 'Реестр компьютеров и назначение групп'],
    groups: ['Группы доступа', 'Видимость устройств для сотрудников'],
    audit: ['Журнал действий', 'История административных изменений'],
    server: ['Сервер', 'Информация о текущей установке']
  };
  var el = function (id) { return document.getElementById(id); };
  var safe = function (s) { return String(s === null || s === undefined ? '' : s).replace(/[&<>"']/g, function (c) { return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]; }); };
  var asList = function (value) { return Array.isArray(value) ? value : []; };
  var date = function (v) { if (!v) return '—'; var d = new Date(Number(v) * 1000); return isNaN(d.getTime()) ? '—' : d.toLocaleString('ru-RU'); };
  var badge = function (name, kind) { return '<span class="pill ' + (kind || '') + '">' + safe(name) + '</span>'; };
  var q = function (value) { return encodeURIComponent(String(value)); };
  var toastTimer = null;
  function toast(message, bad) {
    var box = el('toast');
    box.textContent = message;
    box.classList.toggle('bad', !!bad);
    box.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { box.hidden = true; }, 4200);
  }
  function setPasswordVisible(input, toggle, visible) {
    input.type = visible ? 'text' : 'password';
    toggle.textContent = visible ? 'Скрыть' : 'Показать';
    toggle.setAttribute('aria-label', visible ? 'Скрыть пароль' : 'Показать пароль');
    toggle.title = visible ? 'Скрыть пароль' : 'Показать пароль';
    toggle.setAttribute('aria-pressed', String(visible));
  }
  function showLogin(message) {
    setPasswordVisible(el('password'), document.querySelector('#loginView .password-toggle'), false);
    el('appView').hidden = true;
    el('loginView').hidden = false;
    el('password').value = '';
    el('loginError').hidden = !message;
    el('loginError').textContent = message || '';
    el('username').focus();
  }
  function clearSession() {
    token = '';
    account = null;
    sessionStorage.removeItem('mirvdesk_admin_token');
  }
  async function call(path, method, data) {
    var headers = { 'Accept': 'application/json' };
    if (token) headers.Authorization = 'Bearer ' + token;
    if (data !== undefined) headers['Content-Type'] = 'application/json';
    var options = { method: method || 'GET', headers: headers, cache: 'no-store', credentials: 'omit' };
    if (data !== undefined) options.body = JSON.stringify(data);
    var result = await fetch(path, options);
    var payload = await result.json().catch(function () { return {}; });
    if (result.status === 401 && token && path !== '/api/login') {
      clearSession();
      showLogin('Сеанс завершён. Войдите снова.');
      throw new Error('Сеанс завершён');
    }
    if (!result.ok) throw new Error(payload.error || ('Ошибка HTTP ' + result.status));
    return payload;
  }
  async function readHealth() {
    var result = await fetch('/healthz', { cache: 'no-store', credentials: 'omit' });
    // /healthz returns 503 with useful per-service flags if one child is down.
    return result.json();
  }
  async function all(path) {
    var items = [], page = 1, total;
    do {
      var res = await call(path + (path.indexOf('?') >= 0 ? '&' : '?') + 'current=' + page + '&pageSize=500');
      var part = asList(res.data);
      items = items.concat(part);
      total = Number(res.total || 0);
      if (!part.length || page > 100) break;
      page += 1;
    } while (items.length < total);
    return items;
  }
  async function login(e) {
    e.preventDefault();
    var button = el('loginButton');
    var error = el('loginError');
    error.hidden = true;
    button.disabled = true;
    try {
      var result = await call('/api/login', 'POST', {
        username: el('username').value.trim(),
        password: el('password').value,
        type: 'account'
      });
      if (!result.user || !result.user.is_admin) {
        if (result.access_token) {
          token = result.access_token;
          await call('/api/logout', 'POST').catch(function () {});
        }
        clearSession();
        throw new Error('Для доступа нужны права администратора');
      }
      token = result.access_token;
      account = result.user;
      sessionStorage.setItem('mirvdesk_admin_token', token);
      el('password').value = '';
      signedIn();
      await refresh();
    } catch (err) {
      clearSession();
      showLogin(err.message);
    } finally {
      button.disabled = false;
    }
  }
  function signedIn() {
    el('loginView').hidden = true;
    el('appView').hidden = false;
    el('accountName').textContent = account.display_name || account.name;
    el('avatar').textContent = (account.name || 'A').slice(0, 1).toUpperCase();
    navigate(view);
  }
  async function resume() {
    if (!token) { showLogin(); return; }
    try {
      var user = await call('/api/currentUser', 'POST');
      if (!user.is_admin) throw new Error('Недостаточно прав администратора');
      account = user;
      signedIn();
      await refresh();
    } catch (err) {
      clearSession();
      showLogin(err.message);
    }
  }
  async function logout() {
    try { if (token) await call('/api/logout', 'POST'); }
    catch (_) {}
    clearSession();
    showLogin();
  }
  function navigate(next) {
    if (!titles[next]) return;
    view = next;
    document.querySelectorAll('.nav-item').forEach(function (item) { item.classList.toggle('active', item.dataset.view === next); });
    el('viewTitle').textContent = titles[view][0];
    el('viewSubtitle').textContent = titles[view][1];
    el('breadcrumb').textContent = 'MIRVDESK / ' + titles[view][0].toUpperCase();
    el('search').value = '';
    el('search').hidden = (view === 'overview' || view === 'server');
    render();
  }
  async function refresh() {
    el('refresh').disabled = true;
    try {
      var paths = ['/api/admin/users', '/api/admin/devices', '/api/admin/groups', '/api/admin/audit'];
      var results = await Promise.all([all(paths[0]), all(paths[1]), all(paths[2]), all(paths[3]), readHealth().catch(function () { return { ok: false, hbbs: false, hbbr: false }; }), call('/api/version')]);
      state.users = results[0];
      state.devices = results[1];
      state.groups = results[2];
      state.audit = results[3];
      state.health = results[4];
      state.version = results[5];
      render();
    } catch (err) {
      toast('Не удалось обновить данные: ' + err.message, true);
      el('content').innerHTML = '<div class="panel"><p class="error">' + safe(err.message) + '</p></div>';
    } finally {
      el('refresh').disabled = false;
    }
  }
  function button(label, action, id, danger) {
    return '<button type="button" class="secondary tiny ' + (danger ? 'danger' : '') + '" data-action="' + safe(action) + '" data-id="' + safe(id) + '">' + safe(label) + '</button>';
  }
  function matching(items, fn) {
    var term = el('search').value.trim().toLocaleLowerCase();
    return term ? items.filter(function (item) { return fn(item).toLocaleLowerCase().indexOf(term) !== -1; }) : items;
  }
  function table(headers, rows) {
    return '<div class="table-scroll"><table><thead><tr>' + headers.map(function (v) { return '<th>' + safe(v) + '</th>'; }).join('') + '</tr></thead><tbody>' + (rows.length ? rows.join('') : '<tr><td colspan="' + headers.length + '" class="empty">Нет записей</td></tr>') + '</tbody></table></div>';
  }
  function row(cells) { return '<tr>' + cells.map(function (v) { return '<td>' + v + '</td>'; }).join('') + '</tr>'; }
  function actions(content) { return '<div class="flex-row">' + content + '</div>'; }
  function panel(title, html) { return '<section class="panel"><h2>' + safe(title) + '</h2>' + html + '</section>'; }
  function overview() {
    var online = state.devices.filter(function (d) { return d.online === true; }).length;
    var enabled = state.users.filter(function (u) { return u.status === 1; }).length;
    var healthy = !!(state.health && state.health.ok);
    var metrics = [
      ['Зарегистрировано устройств', state.devices.length, 'В реестре MirvDesk'],
      ['Подключённых сейчас', online, 'По подписанным heartbeat'],
      ['Учётных записей', state.users.length, 'Активных: ' + enabled],
      ['Групп', state.groups.length, 'Для организации доступа']
    ];
    var out = '<div class="metrics">' + metrics.map(function (m) { return '<div class="metric"><div class="label">' + safe(m[0]) + '</div><div class="number">' + safe(m[1]) + '</div><div class="sub">' + safe(m[2]) + '</div></div>'; }).join('') + '</div>';
    var activity = state.audit.slice(0, 7).map(function (a) { return row(['<strong>' + safe(a.actor) + '</strong><span class="dim">' + date(a.created_at) + '</span>', safe(a.action), safe(a.target_id)]); });
    var components = '<div class="info-grid"><div>Rendezvous (hbbs)</div><div>' + badge(state.health && state.health.hbbs ? 'Запущен' : 'Недоступен', state.health && state.health.hbbs ? 'good' : 'warn') + '</div><div>Relay (hbbr)</div><div>' + badge(state.health && state.health.hbbr ? 'Запущен' : 'Недоступен', state.health && state.health.hbbr ? 'good' : 'warn') + '</div><div>Состояние</div><div>' + badge(healthy ? 'Работает' : 'Требует внимания', healthy ? 'good' : 'warn') + '</div><div>Версия сервера</div><div>' + safe(state.version && state.version.version) + '</div></div>';
    out += '<div class="panels">' + panel('Последние действия', table(['Администратор', 'Операция', 'Объект'], activity)) + panel('Компоненты сервера', components) + '</div>';
    out += '<p class="hint">Группы MirvDesk управляют видимостью устройств, но не заменяют пароль или подтверждение удалённого подключения. Статус «в сети» определяется по подписанным уведомлениям клиента, а не по последнему входу в аккаунт.</p>';
    return out;
  }
  function users() {
    var list = matching(state.users, function (u) { return [u.name, u.display_name].join(' '); });
    return '<div class="section-head"><h2>Все пользователи <span class="muted">(' + list.length + ')</span></h2><button class="primary" data-action="user-create">+ Добавить пользователя</button></div>' + table(['Логин', 'Имя', 'Роль', 'Состояние', 'Управление'],
      list.map(function (u) { return row(['<strong>' + safe(u.name) + '</strong>', safe(u.display_name || '—'), badge(u.is_admin ? 'Администратор' : 'Пользователь'), badge(u.status === 1 ? 'Активен' : 'Отключён', u.status === 1 ? 'good' : 'warn'),
        '<div class="flex-row">' + button('Пароль', 'user-password', u.name) + button(u.status === 1 ? 'Отключить' : 'Включить', 'user-status', u.name, u.status === 1) + '</div>']); }));
  }
  function devices() {
    var list = matching(state.devices, function (d) { return [d.id, d.display_name, d.note, d.user, d.user_name, asList(d.device_group_names).join(' ')].join(' '); });
    return '<div class="section-head"><h2>Реестр устройств <span class="muted">(' + list.length + ')</span></h2><span class="hint">Список включает устройства, найденные сервером HBBS</span></div>' +
      table(['Устройство', 'Владелец', 'Присутствие', 'Группы', 'Изменено', 'Управление'], list.map(function (d) {
        var gn = asList(d.device_group_names);
        var info = d.info || {};
        return row(['<strong>' + safe(d.display_name || info.hostname || d.id) + '</strong><span class="dim">ID ' + safe(d.id) + (d.note ? ' · ' + safe(d.note) : '') + '</span>',
          safe(d.user || d.user_name || 'Не назначен'), badge(d.online ? 'В сети' : 'Не в сети', d.online ? 'good' : ''),
          gn.length ? '<div class="chips">' + gn.map(function (g) { return badge(g); }).join('') + '</div>' : '<span class="muted">Без группы</span>',
          '<span class="dim">Последний сигнал: ' + date(d.last_seen_at) + '</span><span class="dim">Вход в аккаунт: ' + date(d.last_account_login) + '</span>',
          '<div class="flex-row">' + button('Имя', 'device-rename', d.id) + button('Заметка', 'device-note', d.id) + button('Группы', 'device-groups', d.id) + '</div>']);
      }));
  }
  function groups() {
    var list = matching(state.groups, function (g) { return g.name + ' ' + (g.note || ''); });
    return '<div class="section-head"><h2>Группы устройств <span class="muted">(' + list.length + ')</span></h2><button class="primary" data-action="group-create">+ Новая группа</button></div><div class="grid-groups">' +
      (list.length ? list.map(function (g) {
        var n = state.devices.filter(function (d) { return asList(d.device_group_names).indexOf(g.name) >= 0; }).length;
        return '<div class="panel group-card"><div class="eyebrow">ГРУППА</div><h3>' + safe(g.name) + '</h3><p>' + n + ' устройств · Управление видимостью</p><div class="buttons">' + button('Участники', 'group-members', g.name) + button('Переименовать', 'group-rename', g.name) + button('Удалить', 'group-delete', g.name, true) + '</div></div>';
      }).join('') : '<div class="panel empty">Группы ещё не созданы</div>') + '</div><p class="hint">Участие в группе предоставляет видимость устройства в адресной книге, а не разрешение на подключение без пароля.</p>';
  }
  function audit() {
    var list = matching(state.audit, function (a) { return [a.actor, a.action, a.target_type, a.target_id].join(' '); });
    return '<div class="section-head"><h2>Административные события <span class="muted">(' + list.length + ')</span></h2><span class="hint">Последние 1000 записей</span></div>' +
      table(['Время', 'Администратор', 'Действие', 'Объект'], list.map(function (a) { return row([date(a.created_at), safe(a.actor), badge(a.action), safe(a.target_type + ': ' + a.target_id)]); }));
  }
  function server() {
    var h = state.health || {}, v = state.version || {};
    return panel('Состояние службы', '<div class="info-grid"><div>Версия MirvDesk</div><div>' + safe(v.version || '—') +
      '</div><div>Версия RustDesk Server</div><div>' + safe(v.rustdesk_server || '—') +
      '</div><div>Rendezvous (hbbs)</div><div>' + badge(h.hbbs ? 'Работает' : 'Недоступен', h.hbbs ? 'good' : 'warn') +
      '</div><div>Relay (hbbr)</div><div>' + badge(h.hbbr ? 'Работает' : 'Недоступен', h.hbbr ? 'good' : 'warn') +
      '</div><div>HTTP API</div><div>' + badge('Доступен', 'good') + '</div></div>') +
      panel('Обслуживание и безопасность', '<p>Панель встроена в основной контейнер и использует существующий API MirvDesk. Для изменения сетевых параметров, восстановления доступа администратора и обновления контейнера используются Docker / Portainer и <code>mirvdesk-admin</code>.</p><p class="muted">Административные действия разрешены только с действительным токеном учётной записи администратора. Сеанс панели хранится только в текущей вкладке браузера.</p>');
  }
  function render() {
    if (!account) return;
    var views = { overview: overview, users: users, devices: devices, groups: groups, audit: audit, server: server };
    el('content').innerHTML = views[view]();
  }
  function showModal(title, fields, submit) {
    el('modalTitle').textContent = title;
    var body = el('modalBody');
    body.innerHTML = '';
    var form = document.createElement('form');
    form.id = 'editForm';
    fields.forEach(function (field) {
      var label = document.createElement('label');
      label.htmlFor = 'field-' + field.name;
      label.textContent = field.title;
      form.appendChild(label);
      var input = document.createElement(field.type === 'textarea' ? 'textarea' : field.type === 'select' ? 'select' : 'input');
      input.id = 'field-' + field.name;
      input.name = field.name;
      input.required = !!field.required;
      if (field.type !== 'select' && field.type !== 'textarea') input.type = field.type || 'text';
      if (field.autocomplete) input.autocomplete = field.autocomplete;
      if (field.maxLength) input.maxLength = field.maxLength;
      if (field.type === 'select') {
        (field.options || []).forEach(function (option) {
          var o = document.createElement('option');
          o.value = option.value;
          o.textContent = option.label;
          input.appendChild(o);
        });
      } else if (field.type === 'textarea') { input.rows = 3; }
      input.value = field.value === undefined ? '' : field.value;
      if (field.type === 'password') {
        var wrapper = document.createElement('div');
        wrapper.className = 'password-field';
        var toggle = document.createElement('button');
        toggle.type = 'button';
        toggle.className = 'password-toggle';
        toggle.dataset.passwordTarget = input.id;
        setPasswordVisible(input, toggle, false);
        wrapper.appendChild(input);
        wrapper.appendChild(toggle);
        form.appendChild(wrapper);
      } else {
        form.appendChild(input);
      }
      if (field.hint) {
        var p = document.createElement('p');
        p.className = 'hint'; p.textContent = field.hint;
        form.appendChild(p);
      }
    });
    var line = document.createElement('p'); line.className = 'form-error'; line.hidden = true; line.setAttribute('role', 'alert'); form.appendChild(line);
    var actions = document.createElement('div'); actions.className = 'modal-actions';
    var cancel = document.createElement('button'); cancel.type = 'button'; cancel.className = 'secondary'; cancel.textContent = 'Отмена'; cancel.addEventListener('click', closeModal); actions.appendChild(cancel);
    var save = document.createElement('button'); save.type = 'submit'; save.className = 'primary'; save.textContent = 'Сохранить'; actions.appendChild(save); form.appendChild(actions);
    form.addEventListener('submit', async function (event) {
      event.preventDefault();
      save.disabled = true;
      line.hidden = true;
      var values = {};
      fields.forEach(function (f) { values[f.name] = form.elements.namedItem(f.name).value; });
      try {
        await submit(values);
        closeModal();
        await refresh();
        toast('Изменения сохранены');
      } catch (err) {
        line.textContent = err.message;
        line.hidden = false;
      } finally { save.disabled = false; }
    });
    body.appendChild(form);
    el('modal').showModal();
    var focus = form.querySelector('input, textarea, select');
    if (focus) focus.focus();
  }
  function closeModal() { if (el('modal').open) el('modal').close(); }
  function getDevice(id) { return state.devices.find(function (d) { return d.id === id; }); }
  function getUser(name) { return state.users.find(function (u) { return u.name === name; }); }
  function getGroup(name) { return state.groups.find(function (g) { return g.name === name; }); }
  async function modify(action, id) {
    if (action === 'user-create') {
      return showModal('Новый пользователь', [
        {name:'username',title:'Логин',required:true,maxLength:80,autocomplete:'off'},
        {name:'display_name',title:'Отображаемое имя'},
        {name:'password',title:'Пароль (не менее 10 символов)',type:'password',required:true,autocomplete:'new-password'}
      ], function (v) {
        if (v.password.length < 10) throw new Error('Минимальная длина пароля — 10 символов');
        return call('/api/admin/users','POST',v);
      });
    }
    if (action === 'user-password') {
      return showModal('Сброс пароля: ' + id, [
        {name:'password',title:'Новый пароль (не менее 10 символов)',type:'password',required:true,autocomplete:'new-password',hint:'После смены пароля все активные сеансы пользователя будут завершены.'}
      ], function (v) { if(v.password.length < 10) throw new Error('Минимальная длина пароля — 10 символов'); return call('/api/admin/users/' + q(id) + '/password','PUT',v); });
    }
    if (action === 'user-status') {
      var u = getUser(id);
      if (!u) return;
      var enabled = u.status !== 1;
      if (!confirm((enabled ? 'Включить' : 'Отключить') + ' пользователя ' + id + '?')) return;
      await call('/api/admin/users/' + q(id) + '/status','PATCH',{enabled: enabled});
      await refresh(); toast('Состояние пользователя изменено'); return;
    }
    if (action === 'group-create' || action === 'group-rename') {
      return showModal(action === 'group-create' ? 'Новая группа' : 'Переименовать группу', [
        {name:'name',title:'Название группы',required:true,maxLength:80,value: action === 'group-rename' ? id : ''}
      ], function (v) { return action === 'group-create' ? call('/api/admin/groups','POST',v) : call('/api/admin/groups/' + q(id),'PUT',v); });
    }
    if (action === 'group-delete') {
      if (!confirm('Удалить группу «' + id + '»? Назначения устройств и членство в этой группе будут удалены.')) return;
      await call('/api/admin/groups/' + q(id),'DELETE');
      await refresh(); toast('Группа удалена'); return;
    }
    if (action === 'group-members') return membersModal(id);
    if (action === 'device-rename' || action === 'device-note') {
      var d = getDevice(id);
      if (!d) return;
      var rename = action === 'device-rename';
      return showModal((rename ? 'Имя устройства: ' : 'Заметка: ') + id, [
        {name:rename ? 'name' : 'note',title:rename ? 'Отображаемое имя' : 'Заметка администратора',type:rename ? 'text' : 'textarea',maxLength:rename ? 100 : 1000,value:rename ? (d.display_name || '') : (d.note || '')}
      ], function (v) {return call('/api/admin/devices/' + q(id) + (rename ? '/display-name' : '/note'),'PUT',v);});
    }
    if (action === 'device-groups') return deviceGroupsModal(id);
  }
  async function membersModal(name) {
    var data = await all('/api/admin/groups/' + q(name) + '/members');
    var modal = el('modal'), body = el('modalBody');
    el('modalTitle').textContent = 'Участники: ' + name;
    body.innerHTML = '<p class="hint">Пользователи в группе могут видеть назначенные ей устройства.</p><div id="membersList"></div><div class="add-member"><select id="newMember" aria-label="Добавить пользователя"></select><button class="primary" id="memberAdd">Добавить</button></div>';
    var draw = function () {
      var list = el('membersList');
      list.innerHTML = data.length ? data.map(function (u) { return '<div class="member-row"><span>' + safe(u.display_name || u.name) + ' <small class="muted">(' + safe(u.name) + ')</small></span>' + button('Исключить', 'member-remove', u.name, true) + '</div>'; }).join('') : '<p class="muted">В группе нет участников.</p>';
      var select = el('newMember');
      select.innerHTML = '';
      state.users.filter(function (u) { return !data.some(function (m) { return m.name === u.name; }); }).forEach(function (u) {
        var option = document.createElement('option'); option.value = u.name; option.textContent = u.name; select.appendChild(option);
      });
      el('memberAdd').disabled = !select.options.length;
    };
    draw();
    body.onclick = async function (event) {
      var b = event.target.closest('button[data-action="member-remove"]');
      if (!b || !confirm('Исключить ' + b.dataset.id + ' из группы «' + name + '»?')) return;
      try { await call('/api/admin/groups/' + q(name) + '/members/' + q(b.dataset.id),'DELETE'); data = await all('/api/admin/groups/' + q(name) + '/members'); draw(); toast('Участник исключён'); await refresh(); }
      catch (err) { toast(err.message,true); }
    };
    el('memberAdd').onclick = async function () {
      var select = el('newMember'); if (!select.value) return;
      try { await call('/api/admin/groups/' + q(name) + '/members','POST',{username:select.value}); data = await all('/api/admin/groups/' + q(name) + '/members'); draw(); toast('Участник добавлен'); await refresh(); }
      catch (err) { toast(err.message,true); }
    };
    modal.showModal();
  }
  function deviceGroupsModal(id) {
    var d = getDevice(id);
    if (!d) return;
    var current = asList(d.device_group_names);
    el('modalTitle').textContent = 'Группы устройства ' + id;
    el('modalBody').innerHTML = '<p class="hint">Выберите группы, которым устройство будет видно в адресных книгах.</p><form id="groupsForm"><div id="deviceGroupChoices" class="check-list"></div><div class="modal-actions"><button type="button" id="groupCancel" class="secondary">Отмена</button><button class="primary" type="submit">Сохранить</button></div></form>';
    var area = el('deviceGroupChoices');
    state.groups.forEach(function (g) {
      var label = document.createElement('label');label.className = 'check-row';
      var input = document.createElement('input');input.type='checkbox';input.value = g.name;input.checked = current.indexOf(g.name) >= 0;
      label.appendChild(input);label.appendChild(document.createTextNode(g.name));area.appendChild(label);
    });
    if (!state.groups.length) area.textContent = 'Группы ещё не созданы.';
    el('groupCancel').onclick = closeModal;
    el('groupsForm').onsubmit = async function (event) {
      event.preventDefault();
      var groups = Array.from(area.querySelectorAll('input:checked')).map(function (c) { return c.value; });
      try { await call('/api/admin/devices/' + q(id) + '/groups','PUT',{groups:groups});closeModal();await refresh();toast('Группы устройства обновлены'); }
      catch (err) { toast(err.message,true); }
    };
    el('modal').showModal();
  }
  document.addEventListener('click', function (event) {
    var toggle = event.target.closest('button[data-password-target]');
    if (toggle) {
      var input = document.getElementById(toggle.dataset.passwordTarget);
      if (input) setPasswordVisible(input, toggle, input.type === 'password');
      return;
    }
    var nav = event.target.closest('[data-view]');
    if (nav) { navigate(nav.dataset.view); return; }
    var b = event.target.closest('button[data-action]');
    if (b && !el('modal').contains(b)) {
      modify(b.dataset.action,b.dataset.id).catch(function (err) { toast(err.message,true); });
    }
  });
  el('loginForm').addEventListener('submit',login);
  el('logout').addEventListener('click',logout);
  el('refresh').addEventListener('click',refresh);
  el('search').addEventListener('input',render);
  el('modalClose').addEventListener('click',closeModal);
  el('modal').addEventListener('close',function () {el('modalBody').onclick = null;});
  resume();
})();