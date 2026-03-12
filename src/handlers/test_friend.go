package handlers

import (
	"net/http"
)

// TestFriendPage serves a simple HTML page for manually testing friend requests and groups.
func TestFriendPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <title>Test Panel</title>
  <style>
    * { box-sizing: border-box; }
    body { font-family: system-ui, -apple-system, BlinkMacSystemFont, sans-serif; background: #0f172a; color: #e5e7eb; padding: 24px; max-width: 860px; margin: 0 auto; }
    h1 { margin-bottom: 4px; }
    h2 { margin: 28px 0 8px; color: #facc15; border-bottom: 1px solid #1e293b; padding-bottom: 6px; }
    p  { color: #94a3b8; margin-top: 0; }
    label { display: block; margin-top: 12px; margin-bottom: 4px; font-size: 13px; color: #94a3b8; }
    input[type="text"], input[type="number"] {
      width: 280px; padding: 6px 8px; border-radius: 4px;
      border: 1px solid #334155; background: #020617; color: #e5e7eb;
    }
    .btn { margin-top: 10px; padding: 8px 16px; border: none; border-radius: 4px; background: #facc15; color: #111827; cursor: pointer; font-weight: 600; font-size: 13px; }
    .btn.secondary { background: #3b82f6; color: #fff; margin-left: 8px; }
    .btn:disabled { opacity: 0.4; cursor: default; }
    pre { background: #020617; border: 1px solid #1e293b; padding: 10px; border-radius: 6px; overflow: auto; font-size: 11px; max-height: 240px; }
    .log { background: #020617; border: 1px solid #1e293b; padding: 10px; border-radius: 6px; font-size: 11px; max-height: 300px; overflow-y: auto; }
    .log div { padding: 2px 0; border-bottom: 1px solid #1e293b; }
    .ok  { color: #4ade80; }
    .err { color: #f87171; }
    .row { margin-top: 14px; }
    section { background: #0f2240; border: 1px solid #1e3a5f; border-radius: 8px; padding: 16px 20px; margin-top: 20px; }
  </style>
</head>
<body>
  <h1>🛠 Dev Test Panel</h1>
  <p>Quick helpers for testing friends &amp; groups. All actions create real DB records.</p>

  <!-- ───────── SECTION 1: Friend Request ───────── -->
  <section>
    <h2>① Send Friend Request</h2>
    <p>Creates a new guest, then sends a friend request to the friend code below.</p>

    <label for="targetFriend">Target friend code</label>
    <input id="targetFriend" type="text" placeholder="e.g. 3x7k2m"/>
    <br/>
    <button class="btn" onclick="sendFriendRequest()">Create guest &amp; send request</button>

    <div class="row"><h3 style="font-size:13px;margin-bottom:4px;">New guest</h3><pre id="friendGuestOut">{}</pre></div>
    <div class="row"><h3 style="font-size:13px;margin-bottom:4px;">Request response</h3><pre id="friendReqOut">{}</pre></div>
  </section>

  <!-- ───────── SECTION 2: Create Group + Invite ───────── -->
  <section>
    <h2>② Create Group &amp; Invite a User</h2>
    <p>Creates a new guest (owner), creates a group, befriends the target, then invites them.</p>

    <label for="groupName">Group name</label>
    <input id="groupName" type="text" placeholder="e.g. Test Squad"/>

    <label for="inviteTarget">Target friend code (must be an existing user)</label>
    <input id="inviteTarget" type="text" placeholder="e.g. 3x7k2m"/>

    <br/>
    <button class="btn" onclick="createGroupAndInvite()">Create guest → group → invite</button>

    <div class="row"><h3 style="font-size:13px;margin-bottom:4px;">Log</h3><div class="log" id="groupLog"></div></div>
  </section>

  <!-- ───────── SECTION 3: Add Random Members ───────── -->
  <section>
    <h2>③ Add Random Members to Existing Group</h2>
    <p>Creates N new guest users, befriends the group owner, and each accepts a group invite.<br/>
       Paste the owner token and group id from section ② log above.</p>

    <label for="ownerToken">Owner guest_token</label>
    <input id="ownerToken" type="text" placeholder="paste owner token"/>

    <label for="targetGroupId">Group ID</label>
    <input id="targetGroupId" type="text" placeholder="paste group_id"/>

    <label for="memberCount">Number of random members</label>
    <input id="memberCount" type="number" value="3" min="1" max="20" style="width:80px"/>

    <br/>
    <button class="btn secondary" onclick="addRandomMembers()">Generate &amp; add members</button>

    <div class="row"><h3 style="font-size:13px;margin-bottom:4px;">Log</h3><div class="log" id="membersLog"></div></div>
  </section>

  <!-- ───────── SECTION 5: Send Random Friend Zikir ───────── -->
  <section>
    <h2>⑤ Send Random Friend Zikir</h2>
    <p>Creates a sender guest, creates a random custom zikir, and sends a zikir request to the target user.<br/>
       Friendship is not required — the target user will receive the request via WebSocket immediately.</p>

    <label for="fzTargetCode">Target friend code (your Unity app)</label>
    <input id="fzTargetCode" type="text" placeholder="e.g. 3x7k2m"/>

    <label for="fzSenderToken">Sender guest_token (leave blank to auto-create)</label>
    <input id="fzSenderToken" type="text" placeholder="auto-created if blank"/>

    <label for="fzName">Zikir name</label>
    <input id="fzName" type="text" placeholder="leave blank for random"/>

    <label for="fzPhrase">Zikir phrase</label>
    <input id="fzPhrase" type="text" placeholder="leave blank for random"/>

    <label for="fzCount">Count (target reads)</label>
    <input id="fzCount" type="number" value="33" min="1" style="width:80px"/>

    <br/>
    <button class="btn" onclick="randomFillFriend()">🎲 Randomise fields</button>
    <button class="btn secondary" onclick="sendFriendZikir()">Send zikir to friend</button>

    <div class="row"><h3 style="font-size:13px;margin-bottom:4px;">Log</h3><div class="log" id="fzLog"></div></div>
  </section>

  <!-- ───────── SECTION 4: Send Random Zikir to Group ───────── -->
  <section>
    <h2>④ Send Random Zikir to Group</h2>
    <p>Creates a random custom zikir and requests it for the group. Uses the owner token and group id auto-filled from section ②.</p>

    <label for="zikirOwnerToken">Owner guest_token</label>
    <input id="zikirOwnerToken" type="text" placeholder="paste owner token (auto-filled from ②)"/>

    <label for="zikirGroupId">Group ID</label>
    <input id="zikirGroupId" type="text" placeholder="paste group_id (auto-filled from ②)"/>

    <label for="zikirName">Zikir name</label>
    <input id="zikirName" type="text" placeholder="leave blank for random"/>

    <label for="zikirPhrase">Zikir phrase (how to read)</label>
    <input id="zikirPhrase" type="text" placeholder="leave blank for random"/>

    <label for="zikirCount">Count (target reads)</label>
    <input id="zikirCount" type="number" value="33" min="1" style="width:80px"/>

    <br/>
    <button class="btn" onclick="randomFill()">🎲 Randomise fields</button>
    <button class="btn secondary" onclick="sendZikirToGroup()">Send zikir to group</button>

    <div class="row"><h3 style="font-size:13px;margin-bottom:4px;">Log</h3><div class="log" id="zikirLog"></div></div>
  </section>

<script>
// ─── helpers ───────────────────────────────────────────────────────────────
async function register() {
  const r = await fetch('/api/guest/register', { method: 'POST' });
  if (!r.ok) throw new Error('register failed: ' + await r.text());
  return r.json();
}

async function api(method, path, token, body) {
  const opts = { method, headers: { 'Content-Type': 'application/json' } };
  if (token) opts.headers['X-Guest-Token'] = token;
  if (body)  opts.body = JSON.stringify(body);
  const r = await fetch(path, opts);
  const j = await r.json();
  if (!r.ok) throw new Error(JSON.stringify(j));
  return j;
}

function log(containerId, msg, isErr) {
  const el = document.getElementById(containerId);
  const d  = document.createElement('div');
  d.className = isErr ? 'err' : 'ok';
  d.textContent = (isErr ? '✗ ' : '✓ ') + msg;
  el.appendChild(d);
  el.scrollTop = el.scrollHeight;
}

// ─── Section 1: Friend Request ──────────────────────────────────────────────
async function sendFriendRequest() {
  const target = document.getElementById('targetFriend').value.trim();
  if (!target) { alert('Enter the target friend code.'); return; }
  const btn = event.target;
  btn.disabled = true;
  document.getElementById('friendGuestOut').textContent = '{}';
  document.getElementById('friendReqOut').textContent = '{}';
  try {
    const guest = await register();
    document.getElementById('friendGuestOut').textContent = JSON.stringify(guest, null, 2);
    const req = await api('POST', '/api/friends/request', guest.guest_token, { friend_code: target });
    document.getElementById('friendReqOut').textContent = JSON.stringify(req, null, 2);
  } catch(e) {
    document.getElementById('friendReqOut').textContent = String(e);
  } finally { btn.disabled = false; }
}

// ─── Section 2: Create Group + Invite ──────────────────────────────────────
async function createGroupAndInvite() {
  const groupName  = document.getElementById('groupName').value.trim();
  const targetCode = document.getElementById('inviteTarget').value.trim();
  if (!groupName)  { alert('Enter a group name.');      return; }
  if (!targetCode) { alert('Enter the target friend code.'); return; }

  const btn = event.target;
  btn.disabled = true;
  document.getElementById('groupLog').innerHTML = '';
  const L = (msg, err) => log('groupLog', msg, err);

  try {
    // 1) Create owner guest
    const owner = await register();
    L('Owner created  token=' + owner.guest_token + '  code=' + owner.friend_code);

    // 2) Send friend request to target (mutual — target will get a pending request)
    //    We need them to be friends before inviting. Owner sends, target needs to accept.
    //    Instead: target sends to owner first if we have no accept endpoint here.
    //    Easiest: owner sends to target.
    try {
      const fr = await api('POST', '/api/friends/request', owner.guest_token, { friend_code: targetCode });
      L('Friend request sent to ' + targetCode + ' → ' + JSON.stringify(fr));
    } catch(e) {
      L('Friend request note: ' + e, true);
    }

    // 3) Create group
    const grp = await api('POST', '/api/groups', owner.guest_token, { name: groupName });
    L('Group created  id=' + grp.group_id + '  name=' + grp.name);

    // 4) Try to invite target (will only succeed if they accepted the friend request already)
    try {
      const inv = await api('POST', '/api/groups/invite', owner.guest_token, { group_id: grp.group_id, friend_code: targetCode });
      L('Group invite sent to ' + targetCode + ' → ' + JSON.stringify(inv));
    } catch(e) {
      L('Group invite failed (target must accept the friend request first): ' + e, true);
    }

    L('─ Owner token for section ③ & ④: ' + owner.guest_token);
    L('─ Group id  for section ③ & ④: ' + grp.group_id);
    document.getElementById('ownerToken').value      = owner.guest_token;
    document.getElementById('targetGroupId').value   = grp.group_id;
    document.getElementById('zikirOwnerToken').value = owner.guest_token;
    document.getElementById('zikirGroupId').value    = grp.group_id;

  } catch(e) {
    L(String(e), true);
  } finally { btn.disabled = false; }
}

// ─── Section 3: Add Random Members ─────────────────────────────────────────
async function addRandomMembers() {
  const ownerToken = document.getElementById('ownerToken').value.trim();
  const groupId    = document.getElementById('targetGroupId').value.trim();
  const count      = parseInt(document.getElementById('memberCount').value, 10) || 1;
  if (!ownerToken) { alert('Paste the owner guest_token.'); return; }
  if (!groupId)    { alert('Paste the group_id.');         return; }

  const btn = event.target;
  btn.disabled = true;
  document.getElementById('membersLog').innerHTML = '';
  const L = (msg, err) => log('membersLog', msg, err);

  for (let i = 0; i < count; i++) {
    try {
      // 1) Create random guest
      const member = await register();
      L('[' + (i+1) + '] Member created  code=' + member.friend_code);

      // 2) Member sends friend request to owner (owner registered earlier)
      //    Get owner friend code first
      const ownerMe = await api('GET', '/api/me', ownerToken, null);
      await api('POST', '/api/friends/request', member.guest_token, { friend_code: ownerMe.friend_code });
      L('[' + (i+1) + '] Member → owner friend request sent');

      // 3) Owner accepts member's friend request
      const pendingReqs = await api('GET', '/api/friends/requests', ownerToken, null);
      const req = pendingReqs.requests && pendingReqs.requests.find(r => r.from_friend_code === member.friend_code);
      if (!req) {
        // Try mutual: sometimes already accepted
        L('[' + (i+1) + '] Pending request not found (may be auto-accepted as mutual)', false);
      } else {
        await api('POST', '/api/friends/request/accept', ownerToken, { request_id: req.request_id });
        L('[' + (i+1) + '] Owner accepted friend request from member');
      }

      // 4) Owner invites member to group
      const inv = await api('POST', '/api/groups/invite', ownerToken, { group_id: groupId, friend_code: member.friend_code });
      L('[' + (i+1) + '] Group invite sent  request_id=' + inv.request_id);

      // 5) Member accepts group invite
      const invList = await api('GET', '/api/groups/invites', member.guest_token, null);
      const groupInv = invList.invites && invList.invites.find(inv => inv.group_id === groupId);
      if (!groupInv) {
        L('[' + (i+1) + '] Could not find group invite for member', true);
      } else {
        await api('POST', '/api/groups/invite/accept', member.guest_token, { request_id: groupInv.request_id });
        L('[' + (i+1) + '] Member joined the group ✓');
      }
    } catch(e) {
      L('[' + (i+1) + '] Error: ' + e, true);
    }
  }

  L('Done. ' + count + ' member(s) processed.');
  btn.disabled = false;
}

// ─── Section 5: Send Random Friend Zikir ───────────────────────────────────
function randomFillFriend() {
  document.getElementById('fzName').value   = pick(ZIKIR_NAMES) + ' #' + rand(1, 999);
  document.getElementById('fzPhrase').value = pick(ZIKIR_PHRASES);
  document.getElementById('fzCount').value  = pick(COUNTS);
}

async function sendFriendZikir() {
  const targetCode = document.getElementById('fzTargetCode').value.trim();
  if (!targetCode) { alert('Enter the target friend code.'); return; }

  let   senderToken = document.getElementById('fzSenderToken').value.trim();
  let   name        = document.getElementById('fzName').value.trim();
  let   phrase      = document.getElementById('fzPhrase').value.trim();
  let   count       = parseInt(document.getElementById('fzCount').value, 10) || 33;

  if (!name)   name   = pick(ZIKIR_NAMES) + ' #' + rand(1, 999);
  if (!phrase) phrase = pick(ZIKIR_PHRASES);

  const btn = event.target;
  btn.disabled = true;
  document.getElementById('fzLog').innerHTML = '';
  const L = (msg, err) => log('fzLog', msg, err);

  try {
    // 1) Create sender guest if no token provided
    if (!senderToken) {
      const sender = await register();
      senderToken = sender.guest_token;
      document.getElementById('fzSenderToken').value = senderToken;
      L('Sender created  token=' + senderToken + '  code=' + sender.friend_code);
    }

    // 2) Resolve target user_id by sending them a friend request (side-effect is fine for testing)
    const frResp  = await api('POST', '/api/friends/request', senderToken, { friend_code: targetCode });
    const toUserId = frResp.friend_id;
    L('Target resolved  user_id=' + toUserId + '  code=' + targetCode);

    // 3) Create custom zikir
    const zikirResp = await api('POST', '/api/zikirs/custom', senderToken, {
      name_tr:      name,
      read_tr:      phrase,
      arabic:       phrase,   // server requires arabic; reuse phrase
      target_count: count,
    });
    const zikirId = zikirResp.zikir_id || zikirResp.id;
    L('Custom zikir created  id=' + zikirId + '  name=' + name + '  count=' + count);

    // 4) Send friend zikir request to target
    const sendResp = await api('POST', '/api/zikirs/friend/send', senderToken, {
      to_user_id:   toUserId,
      zikir_type:   'custom',
      zikir_ref:    zikirId,
      target_count: count,
    });
    L('Friend zikir sent!  request_id=' + sendResp.request_id + '  →  target should see it live via WebSocket');
  } catch(e) {
    L('Error: ' + e, true);
  } finally { btn.disabled = false; }
}

// ─── Section 4: Send Random Zikir to Group ─────────────────────────────────
const ZIKIR_NAMES   = ['Sübhanallah','Elhamdülillah','Allahü Ekber','Estağfirullah','La ilahe illallah','Bismillah','Salavat','Ya Rab','Ya Rahîm','Hasbunallah'];
const ZIKIR_PHRASES = ['Sübhânellahi ve bi hamdihî','Elhamdülillâhi Rabbil âlemin','Allâhü ekber','Estağfirullâhe\'l-azîm','Lâ ilâhe illallâhü vahdehû lâ şerîke leh','Bismillâhirrahmânirrahîm','Allâhümme salli alâ seyyidinâ Muhammed','Hasbünallâhü ve ni\'mel vekîl','Sübhânallâhi ve bi hamdihî sübhânallâhi\'l-azîm','Lâ havle ve lâ kuvvete illâ billâhil aliyyil azîm'];
const COUNTS = [33, 99, 100, 1000, 3000];

function pick(arr) { return arr[Math.floor(Math.random() * arr.length)]; }
function rand(min, max) { return Math.floor(Math.random() * (max - min + 1)) + min; }

function randomFill() {
  document.getElementById('zikirName').value   = pick(ZIKIR_NAMES) + ' #' + rand(1, 999);
  document.getElementById('zikirPhrase').value = pick(ZIKIR_PHRASES);
  document.getElementById('zikirCount').value  = pick(COUNTS);
}

async function sendZikirToGroup() {
  const ownerToken = document.getElementById('zikirOwnerToken').value.trim();
  const groupId    = document.getElementById('zikirGroupId').value.trim();
  let   name       = document.getElementById('zikirName').value.trim();
  let   phrase     = document.getElementById('zikirPhrase').value.trim();
  let   count      = parseInt(document.getElementById('zikirCount').value, 10) || 33;

  if (!ownerToken) { alert('Paste the owner guest_token.'); return; }
  if (!groupId)    { alert('Paste the group_id.');         return; }

  // Auto-fill random values if left blank
  if (!name)   name   = pick(ZIKIR_NAMES)   + ' #' + rand(1, 999);
  if (!phrase) phrase = pick(ZIKIR_PHRASES);

  const btn = event.target;
  btn.disabled = true;
  document.getElementById('zikirLog').innerHTML = '';
  const L = (msg, err) => log('zikirLog', msg, err);

  try {
    // 1) Create a custom zikir. arabic is required by the server; reuse phrase.
    const zikirResp = await api('POST', '/api/zikirs/custom', ownerToken, {
      name_tr:      name,
      read_tr:      phrase,
      arabic:       phrase,   // server requires arabic; use phrase as stand-in
      target_count: count,
    });
    const zikirId = zikirResp.zikir_id || zikirResp.id;
    L('Custom zikir created  id=' + zikirId + '  name=' + name + '  count=' + count);

    // 2) Request the zikir for the group
    const reqResp = await api('POST', '/api/groups/zikirs/request', ownerToken, {
      group_id:     groupId,
      zikir_type:   'custom',
      zikir_ref:    zikirId,
      mode:         'pooled',
      target_count: count,
    });
    L('Group zikir request sent  request_id=' + reqResp.request_id + '  mode=' + reqResp.mode);
  } catch(e) {
    L('Error: ' + e, true);
  } finally { btn.disabled = false; }
}
</script>
</body>
</html>`))
}
