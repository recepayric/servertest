# Go Server

A basic HTTP server written in Go for serving static files and API endpoints.

## Features

- Serves static files from the `build` directory (or root as fallback)
- Health check endpoint: `/api/health`
- Hello endpoint: `/api/hello`
- Automatically uses `PORT` environment variable (set by Render)

## Local Development

```bash
cd src
go run main.go
```

The server will start on `http://localhost:8080`

## Build

```bash
cd src
go build -o server main.go
./server
```

## Render Deployment

For Render, you'll need to:

1. **Build Command**: `cd src && go build -o ../server main.go`
2. **Start Command**: `./server`
3. **Environment**: Render automatically sets the `PORT` environment variable

## Endpoints

- `GET /` - Serves static files (index.html, etc.)
- `GET /api/health` - Health check endpoint
- `GET /api/hello` - Hello message endpoint
