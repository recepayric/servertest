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
			"GET /api/debug":                 func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "application/json"); w.Write([]byte(`{"status":"ok","msg":"server running"}`)) },
			"GET /api/health":                handlers.Health,
			"GET /api/db-health":             handlers.DBHealth,
			"GET /api/zikirs":                handlers.Zikirs,
			"POST /api/guest/register":       handlers.GuestRegister,
			"GET /api/me":                    handlers.Me,
			"GET /api/friends":               handlers.FriendsList,
			"POST /api/friends/remove":       handlers.FriendsRemove,
			"POST /api/friends/request":      handlers.FriendsRequest,
			"POST /api/friends/request/accept":  handlers.FriendsRequestAccept,
			"POST /api/friends/request/refuse":  handlers.FriendsRequestRefuse,
			"GET /api/friends/requests":      handlers.FriendsRequestList,
			"GET /api/friends/requests/sent": handlers.FriendsRequestListSent,
			"POST /api/groups":               handlers.GroupsCreate,
			"GET /api/groups":                handlers.GroupsList,
			"GET /api/groups/members":        handlers.GroupsMembers,
			"POST /api/groups/invite":        handlers.GroupsInvite,
			"POST /api/groups/invite/accept": handlers.GroupsInviteAccept,
			"POST /api/groups/invite/refuse": handlers.GroupsInviteRefuse,
			"GET /api/groups/invites":        handlers.GroupsInviteList,
			"GET /api/groups/invites/sent":   handlers.GroupsInviteListSent,
			"POST /api/groups/kick":          handlers.GroupsKick,
			"GET /ws":    handlers.WebSocket,
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
		log.Printf("❌ 404 no match for %s %s", method, path)
		http.NotFound(writeTo, req)
		return
	}

	r.fileServer.ServeHTTP(writeTo, req)
}
