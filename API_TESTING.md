# API Testing Guide

## ✅ REST API on Render Free Plan

**Yes, REST APIs work perfectly on Render's free plan!** The only limitation is **WebSockets** (which require a paid plan).

## Available Endpoints

Your Go server exposes these REST API endpoints:

- **`GET /api/health`** - Health check endpoint
- **`GET /api/hello`** - Hello message endpoint

## Testing Methods

### 1. **Browser Testing (Easiest)**

Just open your deployed site and click the test buttons on the homepage! The page includes interactive API testing.

### 2. **Browser Address Bar**

Simply visit these URLs in your browser:
- `https://your-app.onrender.com/api/health`
- `https://your-app.onrender.com/api/hello`

### 3. **Using curl (Command Line)**

```bash
# Test health endpoint
curl https://your-app.onrender.com/api/health

# Test hello endpoint
curl https://your-app.onrender.com/api/hello

# Pretty print JSON response
curl https://your-app.onrender.com/api/health | python -m json.tool
```

### 4. **Using PowerShell (Windows)**

```powershell
# Test health endpoint
Invoke-RestMethod -Uri "https://your-app.onrender.com/api/health"

# Test hello endpoint
Invoke-RestMethod -Uri "https://your-app.onrender.com/api/hello"
```

### 5. **Using JavaScript/Fetch (Browser Console)**

Open browser console (F12) and run:

```javascript
// Test health endpoint
fetch('/api/health')
  .then(res => res.json())
  .then(data => console.log(data));

// Test hello endpoint
fetch('/api/hello')
  .then(res => res.json())
  .then(data => console.log(data));
```

### 6. **Using Postman or Insomnia**

1. Create a new GET request
2. URL: `https://your-app.onrender.com/api/health`
3. Send request
4. View JSON response

## Local Testing

Before deploying to Render, test locally:

```bash
# Start the server
cd src
go run main.go

# In another terminal, test the endpoints
curl http://localhost:8080/api/health
curl http://localhost:8080/api/hello
```

## Expected Responses

### `/api/health`
```json
{
  "status": "ok",
  "service": "go-server"
}
```

### `/api/hello`
```json
{
  "message": "Hello from Go server!",
  "language": "Go"
}
```

## Adding More Endpoints

To add more REST API endpoints, edit `src/main.go`:

```go
mux.HandleFunc("/api/your-endpoint", yourHandler)

func yourHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w, `{"key":"value"}`)
}
```

## Notes

- ✅ **REST APIs**: Fully supported on free plan
- ❌ **WebSockets**: Not supported on free plan (requires paid plan)
- ✅ **Static Files**: Fully supported
- ✅ **HTTPS**: Automatically enabled on Render
