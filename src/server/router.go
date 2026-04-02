package server

import (
	"log"
	"net/http"
	"os"
	"strings"

	"servertest/handlers"
)

// NewMux wires all HTTP routes and static file serving.
func NewMux() http.Handler {
	// Use custom router to avoid any ServeMux path-matching quirks
	r := &router{
		routes: map[string]http.HandlerFunc{
			// Simple dev/test helper page
			"GET /admin/test-friend": handlers.TestFriendPage,
			"GET /api/debug": func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"status":"ok","msg":"server running"}`))
			},
			"GET /api/health":                         handlers.Health,
			"GET /api/db-health":                      handlers.DBHealth,
			"GET /api/zikirs":                         handlers.Zikirs,
			"POST /api/zikirs/custom":                 handlers.CustomZikirCreate,
			"GET /api/zikirs/custom":                  handlers.CustomZikirList,
			"GET /api/zikirs/custom/get":              handlers.CustomZikirGet,
			"DELETE /api/zikirs/custom":               handlers.CustomZikirDelete,
			"POST /api/zikirs/friend/send":            handlers.FriendZikirSend,
			"GET /api/zikirs/friend/requests":         handlers.FriendZikirRequestsList,
			"POST /api/zikirs/friend/accept":          handlers.FriendZikirAccept,
			"POST /api/zikirs/friend/refuse":          handlers.FriendZikirRefuse,
			"GET /api/zikirs/friend/sent":             handlers.FriendZikirSentList,
			"GET /api/zikirs/friend":                  handlers.FriendZikirList,
			"POST /api/guest/register":                handlers.GuestRegister,
			"POST /api/auth/link":                     handlers.LinkIdentity,
			"GET /api/me":                             handlers.Me,
			"POST /api/me/display-name":               handlers.UpdateDisplayName,
			"GET /api/friends":                        handlers.FriendsList,
			"POST /api/friends/remove":                handlers.FriendsRemove,
			"POST /api/friends/request":               handlers.FriendsRequest,
			"POST /api/friends/request/accept":        handlers.FriendsRequestAccept,
			"POST /api/friends/request/refuse":        handlers.FriendsRequestRefuse,
			"GET /api/friends/requests":               handlers.FriendsRequestList,
			"GET /api/friends/requests/sent":          handlers.FriendsRequestListSent,
			"POST /api/groups":                        handlers.GroupsCreate,
			"GET /api/groups":                         handlers.GroupsList,
			"GET /api/groups/members":                 handlers.GroupsMembers,
			"POST /api/groups/invite":                 handlers.GroupsInvite,
			"POST /api/groups/invite/accept":          handlers.GroupsInviteAccept,
			"POST /api/groups/invite/refuse":          handlers.GroupsInviteRefuse,
			"GET /api/groups/invites":                 handlers.GroupsInviteList,
			"GET /api/groups/invites/sent":            handlers.GroupsInviteListSent,
			"POST /api/groups/kick":                   handlers.GroupsKick,
			"POST /api/groups/leave":                  handlers.GroupsLeave,
			"GET /api/groups/zikirs":                  handlers.GroupsZikirsList,
			"GET /api/groups/zikirs/detail":           handlers.GroupsZikirDetail,
			"POST /api/groups/zikirs/add":             handlers.GroupsZikirAdd,
			"POST /api/groups/zikirs/remove":          handlers.GroupsZikirRemove,
			"POST /api/groups/zikirs/request":         handlers.GroupsZikirRequest,
			"GET /api/groups/zikirs/requests":         handlers.GroupsZikirRequestsList,
			"POST /api/groups/zikirs/requests/accept": handlers.GroupsZikirRequestAccept,
			"POST /api/groups/zikirs/requests/refuse": handlers.GroupsZikirRequestRefuse,
			"GET /ws":      handlers.WebSocket,
			"GET /ws/echo": handlers.WebSocketEcho,
		},
	}

	// File server for everything else
	staticDir := ".."
	if fi, err := os.Stat("../build/index.html"); err == nil && !fi.IsDir() {
		staticDir = "../build"
	}
	log.Printf("📁 Serving static files from: %s", staticDir)

	r.fileServer = http.StripPrefix("/", http.FileServer(http.Dir(staticDir)))
	return r
}

type router struct {
	routes     map[string]http.HandlerFunc
	fileServer http.Handler
}

func (r *router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	method := req.Method
	incRequestCount()
	log.Printf("📥 %s %s", method, path)

	// WebSocket upgrade uses GET
	if strings.HasPrefix(path, "/ws") {
		method = "GET"
	}

	key := method + " " + path
	// WebSocket needs raw ResponseWriter (Hijacker); use counter for HTTP only
	writeTo := w
	if !strings.HasPrefix(path, "/ws") {
		writeTo = &responseCounter{ResponseWriter: w}
	}
	if h, ok := r.routes[key]; ok {
		h(writeTo, req)
		return
	}

	// Try without trailing slash
	if strings.HasSuffix(path, "/") {
		key2 := method + " " + strings.TrimSuffix(path, "/")
		if h, ok := r.routes[key2]; ok {
			h(writeTo, req)
			return
		}
	}

	// API path not found - log and 404
	if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/ws") {
		log.Printf("❌ 404 no match for %s %s (tried key=%q)", method, path, key)
		http.NotFound(writeTo, req)
		return
	}

	r.fileServer.ServeHTTP(writeTo, req)
}
