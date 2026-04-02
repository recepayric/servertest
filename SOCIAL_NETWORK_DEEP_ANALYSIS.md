# Social + Network Deep Analysis

Last updated: 2026-04-02
Scope: Unity client (`zikirmatik`) + Go backend (`servertest`)

## 1) End-to-End Pipeline

## 1.1 Startup / Identity / Session

1. Unity `ApiClient.Start()` runs `EnsureRegistered()`.
2. If `GuestToken` exists in `PlayerPrefs`, client:
   - connects WS (`WsNotifier.Connect()`),
   - triggers social warmup (`FriendsService.RefreshAll()`).
3. If token does not exist and `autoRegisterOnStart=true`, it calls `POST /api/guest/register`, stores `GuestToken` and `FriendCode`, then connects WS.
4. Optional provider flow (`UgsAuthToServerController`):
   - signs in via UGS,
   - registers/links through `ServerGuestRegistrar`,
   - handles `identity_conflict` with `switch` / `keep`,
   - reconnects WS and refreshes social data.

Expected result:
- Returning user: immediate WS join + cache-first social UI.
- New user: guest user created, token persisted, social initializes with empty data.

---

## 1.2 Social Data Sync (REST, revision-based)

Primary endpoint: `POST /api/social/sync`

Client behavior (`FriendsService.RefreshAll`):
1. Load local social snapshot from `SocialCacheStore` (keyed by guest token).
2. Apply cached friends/requests/groups immediately to UI.
3. Fetch `/api/me` for current profile.
4. Send one sync request with local revs:
   - `friends_rev`
   - `friend_pending_rev`
   - `friend_sent_rev`
   - `groups_rev`
   - `group_pending_rev`
   - `group_sent_rev`
5. Server compares client revs with `user_social_meta` revs.
6. Server returns only changed sections (`*_changed=true` + arrays).
7. Client updates only changed lists and writes new snapshot to `social_cache.json`.
8. If sync fails, client falls back to legacy chain:
   - friends
   - friend pending
   - friend sent
   - groups + members + invites + group zikirs/requests

Expected result:
- First open/login: instant cached render, then silent correction from server.
- Normal open with no changes: one sync call, almost no payload.
- After mutations: only affected sections re-downloaded.

---

## 1.3 Realtime Pipeline (WebSocket)

WS connection:
- Client connects to `wss://.../ws?token=<GuestToken>`.
- Server validates token in `users.guest_token`.
- Server registers connection in WS hub (multi-connection per user supported).

Realtime message path:
1. Server mutation handlers emit WS pushes (`ws.Hub.Push` / `PushToMany`).
2. Unity `WsNotifier.RunAsync()` receives frames.
3. Messages are reassembled safely using `MemoryStream` until `EndOfMessage` (fragment-safe).
4. `HandleMessage()` dispatches to services:
   - friends updates -> `FriendsService`
   - groups updates -> `GroupsService`
   - zikir updates -> `ZikirsService`
5. Services update local in-memory caches and fire UI events.

Expected result:
- Social UI updates even when panel is already open.
- No need for full app restart for friend/group request visibility.
- After offline period, sync endpoint heals missed realtime updates.

---

## 1.4 Revision Counter Lifecycle

Server:
- `ensureSocialMetaRow()` creates `user_social_meta` row lazily.
- `bumpSocialRevs()` increments specific columns per mutation.

Examples:
- send friend request:
  - sender `friend_sent_rev++`
  - recipient `friend_pending_rev++`
- accept friend request:
  - both `friends_rev++`
  - acceptor `friend_pending_rev++`
  - sender `friend_sent_rev++`
- group invite send/accept/refuse:
  - bumps `group_pending_rev`, `group_sent_rev`, `groups_rev` accordingly.

Expected result:
- Client can detect exactly which social slices changed, without N separate polling calls.

---

## 1.5 UI Refresh Strategy (TTL + cache-first)

Implemented pattern:
- Panels rebuild from service cache in `OnEnable`.
- Optional network refresh is gated by:
  - `autoRefreshOnOpen` boolean,
  - `Refresh...IfStale(ttl)` checks.

Panels using this:
- `FriendsPanel`
- `PendingPanel`
- `GroupsPanel`
- `FriendZikirRequests`
- `FriendZikirActivities`
- `GroupDetailPanel` (members and zikirs tabs use stale checks)

Expected result:
- Opening/closing social tabs repeatedly should not spam REST calls.
- Data still gets refreshed when stale or when WS indicates change.

---

## 2) Current State Check (Deep)

## 2.1 What is working correctly

- One-shot social sync endpoint is wired and active (`/api/social/sync`).
- Server revision model (`user_social_meta`) is active.
- Critical WS race in hub is fixed (lock held during sends).
- Fragmented WS frame handling is fixed on Unity client.
- Friend accept/refuse realtime events are emitted and consumed.
- Group leave/kick realtime notifications are emitted.
- 409 conflict handling for friend/group invite flows is correctly reachable in client logic.

---

## 2.2 Important risks / gaps still present

1. **Group member dedupe risk still exists**
   - `GroupsService.NotifyMemberJoined()` appends blindly if group cache exists.
   - Replayed/duplicate WS events can create duplicate members in cache/UI.

2. **Social cache persistence after WS friend updates is incomplete**
   - `NotifyFriendRequestAccepted/Refused` updates memory + UI events.
   - It does not persist `social_cache.json` immediately.
   - After app restart (before next sync), cache can be stale.

3. **Regression present in `GroupDetailPanel`**
   - `OpenZikirDetailPanel` and `CloseZikirDetailPanel` currently return immediately.
   - This disables zikir detail panel open/close flow.

4. **Potential duplicate subscriptions in social panels**
   - `FriendsPanel` subscribes in `Start()` and unsubscribes only in `OnDestroy()`.
   - If object lifecycle is unusual (recreated without destroy), events can stack.
   - Lower severity, but worth hardening with `OnEnable/OnDisable` subscription symmetry.

---

## 3) Social + Network Scenario Expectations

## Scenario A: User opens app (already logged in)

Expected:
1. WS connects successfully.
2. Social panel can render cached lists immediately.
3. One `/api/social/sync` call checks revs.
4. If no changes, no heavy payload.
5. If changes happened while offline, changed slices are returned and UI updates.

---

## Scenario B: User sends friend request

Expected:
1. Sender `AddFriend` returns either:
   - `ok` + pending sent request entry, or
   - `already_sent` (409 path).
2. Recipient receives `friend_request` WS push and pending list updates.
3. Revisions bump:
   - sender `friend_sent_rev`
   - recipient `friend_pending_rev`

---

## Scenario C: Recipient accepts friend request

Expected:
1. Recipient friend list updates locally right after accept API success.
2. Sender receives `friend_request_accepted` WS event.
3. Sender removes sent-pending item and adds new friend in-memory.
4. Both accounts have bumped `friends_rev` for sync consistency.

---

## Scenario D: User is removed from group (kick/leave)

Expected:
- Kicked user receives `group_member_left`.
- Remaining members also receive `group_member_left`.
- Members list can update without full manual refresh.

---

## Scenario E: App is closed during updates

Expected:
- No WS updates while closed.
- On next open, `/api/social/sync` rev check reconciles all missed social changes.

---

## 4) API and Event Map (Practical Reference)

REST:
- `POST /api/social/sync`
- `GET /api/friends`
- `GET /api/friends/requests`
- `GET /api/friends/requests/sent`
- `GET /api/groups`
- `GET /api/groups/invites`
- `GET /api/groups/invites/sent`

WS event types consumed by client:
- `friend_request`
- `friend_request_accepted`
- `friend_request_refused`
- `group_invite`
- `group_member_joined`
- `group_member_left`
- `friend_zikir_request`
- `friend_zikir_accepted`
- `group_zikir_request`
- `group_zikir_added`
- `group_zikir_request_response`
- `zikir_read_update`

---

## 5) Performance Expectations (Current Architecture)

With current design:
- Login/open social cost is dominated by one sync request, not many list calls.
- Realtime path reduces polling frequency for active sessions.
- Cache-first rendering improves perceived responsiveness.
- Revision-based sync keeps payload small when state is unchanged.

Remaining bandwidth/cpu hotspots:
- Group zikir and custom zikir resolution can still create bursts in specific panels.
- Some UI panels still rebuild whole lists on every related event.

---

## 6) Recommended Next Hardening (Small, High ROI)

1. Deduplicate `NotifyMemberJoined` by `user_id`.
2. Persist social cache after WS-driven friend/group mutations (not only after sync).
3. Remove accidental early `return` in `GroupDetailPanel` detail open/close.
4. Standardize event subscriptions to `OnEnable/OnDisable` in panels.
5. Optional: add lightweight telemetry counters
   - sync calls,
   - fallback calls,
   - WS reconnect count,
   - per-panel refresh count.

---

## 7) Quick Verification Checklist

1. Open app with cached token:
   - confirm WS connected,
   - confirm one `/api/social/sync`.
2. Send friend request from device A:
   - pending appears realtime on B.
3. Accept on B:
   - A friend list updates without restart.
4. Kick user from group:
   - kicked user and remaining users update in realtime.
5. Kill app, perform changes on another device, reopen:
   - sync catches up without manual clears.

