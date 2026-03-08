# Zikir App API Reference

Base URL: `http://localhost:8080` (or your deploy URL)

**Auth:** All protected endpoints require `X-Guest-Token: <guest_token>` header.

---

## Table of Contents

1. [Auth & User](#auth--user)
2. [Friends](#friends)
3. [Groups](#groups)
4. [Group Zikirs (Request Flow)](#group-zikirs-request-flow)
5. [Friend Zikirs](#friend-zikirs)
6. [Custom Zikirs](#custom-zikirs)
7. [WebSocket](#websocket)

---

## Auth & User

### Register (Guest)

**POST** `/api/guest/register`

Creates a new user. No body required.

**Response:**
```json
{
  "guest_token": "abc123...",
  "friend_code": "3x7k2m",
  "user_id": "uuid"
}
```

Store `guest_token` and use as `X-Guest-Token` for all later requests.

---

### Get Current User

**GET** `/api/me`  
**Header:** `X-Guest-Token`

**Response:**
```json
{
  "user_id": "uuid",
  "friend_code": "3x7k2m",
  "display_name": "User#3x7k2m"
}
```

---

## Friends

### Send Friend Request

**POST** `/api/friends/request`  
**Body:**
```json
{
  "friend_code": "abc123"
}
```

**Response (pending):**
```json
{
  "status": "ok",
  "request_id": "uuid",
  "friend_id": "uuid",
  "friend_code": "abc123",
  "display_name": "User#abc123"
}
```

**Response (mutual add – auto-accepted):**
```json
{
  "status": "ok",
  "accepted": "mutual",
  "friend_id": "uuid",
  "friend_code": "abc123",
  "display_name": "..."
}
```

**Errors:** `friend_code not found`, `already friends`, `request already sent`

---

### Accept Friend Request

**POST** `/api/friends/request/accept`  
**Body:**
```json
{
  "request_id": "uuid"
}
```

**Response:**
```json
{
  "status": "ok",
  "friend_id": "uuid",
  "friend_code": "abc123",
  "display_name": "..."
}
```

---

### Refuse Friend Request

**POST** `/api/friends/request/refuse`  
**Body:**
```json
{
  "request_id": "uuid"
}
```

**Response:** `{ "status": "ok" }`

---

### List Friends

**GET** `/api/friends`

**Response:**
```json
{
  "friends": [
    {
      "user_id": "uuid",
      "friend_code": "abc123",
      "display_name": "..."
    }
  ]
}
```

---

### List Incoming Friend Requests

**GET** `/api/friends/requests`

**Response:**
```json
{
  "requests": [
    {
      "request_id": "uuid",
      "from_user_id": "uuid",
      "from_friend_code": "abc",
      "from_display_name": "...",
      "status": "pending"
    }
  ]
}
```

---

### List Sent Friend Requests (Pending)

**GET** `/api/friends/requests/sent`

**Response:**
```json
{
  "requests": [
    {
      "request_id": "uuid",
      "friend_id": "uuid",
      "friend_code": "abc",
      "display_name": "..."
    }
  ]
}
```

---

### Remove Friend

**POST** or **DELETE** `/api/friends/remove`  
**Body:**
```json
{
  "friend_id": "uuid"
}
```
or
```json
{
  "friend_code": "abc123"
}
```

**Response:** `{ "status": "ok" }`

---

## Groups

### Create Group

**POST** `/api/groups`  
**Body:**
```json
{
  "name": "My Group"
}
```

**Response:**
```json
{
  "status": "ok",
  "group_id": "uuid",
  "name": "My Group",
  "owner_id": "uuid"
}
```

---

### List Groups

**GET** `/api/groups`

**Response:**
```json
{
  "groups": [
    {
      "group_id": "uuid",
      "name": "...",
      "owner_id": "uuid"
    }
  ]
}
```

---

### List Group Members

**GET** `/api/groups/members?group_id=<uuid>`

**Response:**
```json
{
  "members": [
    {
      "user_id": "uuid",
      "friend_code": "abc",
      "display_name": "...",
      "is_owner": true
    }
  ]
}
```

---

### Invite Friend to Group

**POST** `/api/groups/invite`  
**Body:**
```json
{
  "group_id": "uuid",
  "friend_code": "abc123"
}
```

Owner only. Invitee must be a friend. **Response:** `{ "status": "ok" }`

---

### Accept Group Invite

**POST** `/api/groups/invite/accept`  
**Body:**
```json
{
  "request_id": "uuid"
}
```

---

### Refuse Group Invite

**POST** `/api/groups/invite/refuse`  
**Body:**
```json
{
  "request_id": "uuid"
}
```

---

### List Pending Group Invites (Incoming)

**GET** `/api/groups/invites`

---

### List Sent Group Invites (Pending)

**GET** `/api/groups/invites/sent`

---

### Kick Member

**POST** `/api/groups/kick`  
**Body:**
```json
{
  "group_id": "uuid",
  "user_id": "uuid"
}
```

Owner only. **Response:** `{ "status": "ok" }`

---

### Leave Group

**POST** `/api/groups/leave`  
**Body:**
```json
{
  "group_id": "uuid"
}
```

Owner cannot leave (must transfer or disband first).

---

## Group Zikirs (Request Flow)

**Any member** (including owner) can send a zikir to the group. Everyone sees it. **Each member** accepts or refuses **individually**. Who accepted and who refused is visible to all.

- **24h expiry:** Requests expire 24 hours after creation.
- **Per-member instances:** If you accept, you get your own instance to complete (tap to count). Progress is per user.

---

### Submit Group Zikir Request

**POST** `/api/groups/zikirs/request`  
**Body:**
```json
{
  "group_id": "uuid",
  "zikir_type": "builtin",
  "zikir_ref": "zikir_id_or_ref",
  "mode": "individual",
  "target_count": 100
}
```

- `zikir_type`: `"builtin"` or `"custom"`
- `zikir_ref`: builtin zikir id or custom zikir uuid
- `mode`: `"individual"` (per-member instances; default)
- `target_count`: default 100

**Response:**
```json
{
  "status": "ok",
  "request_id": "uuid"
}
```

Pushes `group_zikir_request` to **all** group members (including sender).

---

### List Group Zikirs (Direct Add – Legacy)

**GET** `/api/groups/zikirs?group_id=<uuid>`

Returns non-expired zikirs added via direct add (`POST /api/groups/zikirs/add`). For the request flow, use the requests list instead.

---

### List Group Zikir Requests

**GET** `/api/groups/zikirs/requests?group_id=<uuid>`

**All group members** see all requests (within 24h). Includes who accepted, who refused, and your own response.

**Response:**
```json
{
  "requests": [
    {
      "request_id": "uuid",
      "group_id": "uuid",
      "from_user_id": "uuid",
      "zikir_type": "custom",
      "zikir_ref": "uuid",
      "mode": "individual",
      "target_count": 100,
      "created_at": "...",
      "friend_code": "abc",
      "display_name": "...",
      "accepted_by": [
        { "user_id": "...", "friend_code": "...", "display_name": "...", "reads": 5 }
      ],
      "refused_by": [
        { "user_id": "...", "friend_code": "...", "display_name": "..." }
      ],
      "my_response": "accepted",
      "my_reads": 5
    }
  ]
}
```

- `accepted_by`: list of users who accepted (with `reads` for each)
- `refused_by`: list of users who refused
- `my_response`: `"accepted"` | `"refused"` | `null`
- `my_reads`: your reads when `my_response` = `"accepted"`

---

### Accept Group Zikir Request (For Yourself)

**POST** `/api/groups/zikirs/requests/accept`  
**Body:**
```json
{
  "request_id": "uuid"
}
```

**Any member** (including sender) can accept for themselves. Creates a per-user instance. Broadcasts `group_zikir_request_response` to all members.

**Response:** `{ "status": "ok" }`

---

### Refuse Group Zikir Request (For Yourself)

**POST** `/api/groups/zikirs/requests/refuse`  
**Body:**
```json
{
  "request_id": "uuid"
}
```

**Any member** can refuse for themselves. Broadcasts `group_zikir_request_response` to all members.

**Response:** `{ "status": "ok" }`

---

### Get Group Zikir/Request Detail

**GET** `/api/groups/zikirs/detail?group_id=<uuid>&request_id=<uuid>`  
or  
**GET** `/api/groups/zikirs/detail?group_id=<uuid>&group_zikir_id=<uuid>`

**Response (request):**
```json
{
  "item_type": "request",
  "request_id": "uuid",
  "group_id": "uuid",
  "from_user": { "user_id": "...", "friend_code": "...", "display_name": "..." },
  "accepted_by": [
    { "user_id": "...", "friend_code": "...", "display_name": "...", "reads": 5 }
  ],
  "refused_by": [
    { "user_id": "...", "friend_code": "...", "display_name": "..." }
  ],
  "progress": { "user_id": 5, "user_id2": 10 }
}
```

**Response (group zikir – direct add):**
```json
{
  "item_type": "zikir",
  "group_zikir_id": "uuid",
  "group_id": "uuid",
  "from_user": { "user_id": "...", "friend_code": "...", "display_name": "..." },
  "accepted_by": null,
  "refused_by": null,
  "progress": { "total": 42 }
}
```

---

### Add Group Zikir (Direct – Legacy)

**POST** `/api/groups/zikirs/add`

Adds directly without approval. Prefer `/api/groups/zikirs/request` for the request flow.

**Body:**
```json
{
  "group_id": "uuid",
  "zikir_type": "custom",
  "zikir_ref": "uuid",
  "mode": "pooled",
  "target_count": 100
}
```

---

### Remove Group Zikir

**POST** `/api/groups/zikirs/remove`  
**Body:**
```json
{
  "group_id": "uuid",
  "zikir_id": "uuid"
}
```

Owner or who added can remove.

---

## Friend Zikirs

Send a zikir to a friend. They accept or refuse. Accepted zikirs appear in their list and can be read.

**24h expiry:** Same as group – requests and accepted zikirs expire 24h after request creation.

---

### Send Friend Zikir

**POST** `/api/zikirs/friend/send`  
**Body:**
```json
{
  "to_user_id": "uuid",
  "zikir_type": "builtin",
  "zikir_ref": "zikir_id",
  "target_count": 33
}
```

**Response:**
```json
{
  "status": "ok",
  "request_id": "uuid"
}
```

---

### List Friend Zikir Requests (Incoming, Pending)

**GET** `/api/zikirs/friend/requests`

**Response:**
```json
{
  "requests": [
    {
      "request_id": "uuid",
      "from_user_id": "uuid",
      "zikir_type": "custom",
      "zikir_ref": "uuid",
      "target_count": 33,
      "created_at": "...",
      "friend_code": "abc",
      "display_name": "..."
    }
  ]
}
```

---

### Accept Friend Zikir

**POST** `/api/zikirs/friend/accept`  
**Body:**
```json
{
  "request_id": "uuid"
}
```

**Response:**
```json
{
  "status": "ok",
  "friend_zikir_id": "uuid"
}
```

---

### Refuse Friend Zikir

**POST** `/api/zikirs/friend/refuse`  
**Body:**
```json
{
  "request_id": "uuid"
}
```

---

### List Accepted Friend Zikirs

**GET** `/api/zikirs/friend`

**Response:**
```json
{
  "zikirs": [
    {
      "id": "uuid",
      "from_user_id": "uuid",
      "zikir_type": "custom",
      "zikir_ref": "uuid",
      "target_count": 33,
      "reads": 5,
      "created_at": "...",
      "friend_code": "abc",
      "display_name": "..."
    }
  ]
}
```

---

## Custom Zikirs

User-created zikirs. Create first, then use `zikir_ref` when adding to groups or sending to friends.

---

### Create Custom Zikir

**POST** `/api/zikirs/custom`  
**Body:**
```json
{
  "name_tr": "Subhanallah",
  "name_en": "",
  "read_tr": "Okunuş",
  "arabic": "سُبْحَانَ اللَّهِ",
  "translation_tr": "Allah'ı tesbih ederim",
  "translation_en": "",
  "description_tr": "...",
  "description_en": "",
  "target_count": 33,
  "category": "tesbih",
  "tags": ["tesbih"]
}
```

Required: `arabic`, `name_tr`.

**Response:**
```json
{
  "status": "ok",
  "zikir_id": "uuid"
}
```

---

### List Custom Zikirs

**GET** `/api/zikirs/custom`  
**GET** `/api/zikirs/custom?id=<uuid>` (single zikir, if owner)

**Response (list):**
```json
{
  "zikirs": [
    {
      "id": "uuid",
      "name_tr": "...",
      "name_en": "",
      "read_tr": "...",
      "arabic": "...",
      "translation_tr": "...",
      "translation_en": "",
      "description_tr": "...",
      "description_en": "",
      "target_count": 33,
      "category": "...",
      "tags": [],
      "created_at": "..."
    }
  ]
}
```

---

### Get Custom Zikir by Ref

**GET** `/api/zikirs/custom/get?ref=<uuid>`

Access if: owner, group member (group has this zikir), group member (group request references it), or friend zikir receiver.

**Response:** Same shape as list item.

---

### Delete Custom Zikir

**DELETE** `/api/zikirs/custom?id=<uuid>`

Owner only.

---

## WebSocket

**URL:** `ws://localhost:8080/ws?token=<guest_token>`

Connect with guest token as query param. Used for real-time pushes and zikir reads.

---

### Sending Zikir Read

Send a JSON message:

```json
{
  "type": "zikir_read",
  "payload": {
    "target": "group",
    "group_zikir_id": "uuid",
    "friend_zikir_id": null,
    "count": 1
  }
}
```

For friend zikir:
```json
{
  "type": "zikir_read",
  "payload": {
    "target": "friend",
    "group_zikir_id": null,
    "friend_zikir_id": "uuid",
    "request_id": null,
    "count": 1
  }
}
```

For group request (per-member instance you accepted):
```json
{
  "type": "zikir_read",
  "payload": {
    "target": "group_request",
    "request_id": "uuid",
    "count": 1
  }
}
```

- `count`: 1–100, default 1
- Expired zikirs are rejected (no DB update, no broadcast)

---

### Push Message Types

All pushes have shape: `{ "type": "...", "payload": { ... } }`

| Type | When |
|------|------|
| `friend_request` | Someone sent you a friend request |
| `group_invite` | Group owner invited you |
| `group_member_joined` | Someone joined a group you're in |
| `group_member_left` | Someone left a group |
| `group_zikir_request` | Member sent a zikir request (all members) |
| `group_zikir_request_response` | Someone accepted or refused a zikir request |
| `group_zikir_added` | Zikir was added (direct add flow) |
| `group_zikir_request_refused` | (Legacy) Owner refused |
| `friend_zikir_request` | Friend sent you a zikir |
| `friend_zikir_accepted` | Friend accepted your zikir |
| `zikir_read_update` | Zikir progress updated (group, friend, or group_request) |

---

### zikir_read_update Payload

**Group (pooled):**
```json
{
  "target": "group",
  "progress": {
    "group_zikir_id": "uuid",
    "mode": "pooled",
    "total": 43
  }
}
```

**Group (individual):**
```json
{
  "target": "group",
  "progress": {
    "group_zikir_id": "uuid",
    "mode": "individual",
    "per_user": { "user_id": 10, ... }
  }
}
```

**Friend:**
```json
{
  "target": "friend",
  "friend_zikir_id": "uuid",
  "reads": 6
}
```

**Group request (per-member):**
```json
{
  "target": "group_request",
  "request_id": "uuid",
  "user_id": "uuid",
  "reads": 6
}
```

---

## Errors

- **401:** Missing or invalid `X-Guest-Token`
- **403:** Not allowed (e.g. not group member, not owner)
- **404:** Resource not found or expired
- **409:** Conflict (e.g. already friends, zikir already in group)

Error body: `{ "error": "message" }`

---

## Quick Reference

| Area | Key Endpoints |
|------|---------------|
| Auth | `POST /api/guest/register`, `GET /api/me` |
| Friends | `POST /api/friends/request`, `GET /api/friends`, accept/refuse |
| Groups | `POST /api/groups`, `GET /api/groups`, invite, members |
| Group zikirs | `POST /api/groups/zikirs/request`, list, requests, accept/refuse, detail |
| Friend zikirs | `POST /api/zikirs/friend/send`, list, accept/refuse |
| Custom zikirs | `POST /api/zikirs/custom`, `GET /api/zikirs/custom/get?ref=` |
| Real-time | `GET /ws?token=`, send `zikir_read`, receive pushes |
