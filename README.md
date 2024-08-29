# Links

An opinionated tool to efficiently manage large amount of repeated bookmarks.

Running locally:

- if you have [`task`](https://taskfile.dev/) use `task run`
- if not, use `go run main.go serve --dir=pb_data --dev`

## Adding to browsers

### Chrome-based (Chrome, Arc, Opera, etc.)

```
Name: Links
Shortcut: l
URL: http://localhost:8090/api/expand?q=%s
```

### Firefox

Open instance of links-ng, ex. locally at http://localhost:8090 and use built-in Firefox's "Add search engine" functionality.

