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

    L('─ Owner token for section ③: ' + owner.guest_token);
    L('─ Group id  for section ③: ' + grp.group_id);
    document.getElementById('ownerToken').value   = owner.guest_token;
    document.getElementById('targetGroupId').value = grp.group_id;

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
</script>
</body>
</html>`))
}
