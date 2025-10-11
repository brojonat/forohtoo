# Web Templates

This directory contains HTML templates that are served by the forohtoo HTTP server.

## Structure

```
web/
├── templates/       # HTML templates (Go html/template format)
│   └── sse-client.html  # SSE transaction streaming demo page
└── README.md
```

## Available Pages

### SSE Transaction Stream Client

A web-based demo page for viewing real-time Solana wallet transactions via Server-Sent Events.

**URLs:**
- `http://localhost:8080/` - Main page
- `http://localhost:8080/stream` - Alternative URL

**Features:**
- Real-time transaction streaming via SSE
- Filter by wallet address or view all wallets
- Clean, responsive UI
- Transaction details including amounts, slots, and timestamps

## Adding New Templates

1. Add your `.html` file to the `web/templates/` directory
2. Use Go's `html/template` syntax for any dynamic content
3. Add a route handler in `service/server/html_handlers.go`
4. Register the route in `service/server/server.go`

## Template Syntax

Templates use Go's standard `html/template` package. Example:

```html
<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}}</title>
</head>
<body>
    <h1>{{.Heading}}</h1>
    {{range .Items}}
        <p>{{.}}</p>
    {{end}}
</body>
</html>
```

## Development

The server automatically loads templates from `web/templates/` on startup. If you modify a template, restart the server to see changes.

For development with hot reloading, consider using `air` (see root Makefile for `make run-dev`).
