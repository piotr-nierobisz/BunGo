# Templates and Layouts

BunGo integrates Go's standard `html/template` library to structure your initial HTML page structure before the React views mount on the DOM.

Since passing a `Template` is mandatory for Page Routes, you might wonder how to prevent duplicating basic HTML structures like `<head>` and `<body>`. The answer is **Layouts**.

## Templates vs Layouts

- **Template (`.gohtml`)**: This is the strictly required HTML specific file for the page content. (e.g. `index.gohtml`)
- **Layout (`.gohtml`)**: This is an optional wrapper file. It defines the generic shell of the application (e.g. `base.gohtml`), creating an area specifically for the Template content to be placed into using Go template directives.

### Defining a Layout
In your `web/layouts/base.gohtml`:
```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>My App</title>
</head>
<body>
    <main>
        {{block "content" .}}{{end}}
    </main>
</body>
</html>
```

### Defining a Template
In your `web/layouts/index.gohtml`, you simply define the `content` block to drop your markup and Mount roots into the layout!
```html
{{define "content"}}
    <div id="root"></div> <!-- Your React app will mount here! -->
{{end}}
```

### Connecting it
In your Go server:
```go
// apply default layout on all templates
srv.SetDefaultLayout("base.gohtml")
```

Or on a specific page:
```go
srv.Page(bungo.PageRoute{
    Path:     "/",
    Template: "index.gohtml",
    Layout:   "base.gohtml", // Overrides default
})
```

## Using Go data in your Templates
The map returned from your Go Handler is accessible directly in the `.gohtml` files. So if you return `{"PageTitle": "Hello"}`, you can write `<title>{{.PageTitle}}</title>` inside your Layout or Template!

_Wait, what about the `<script>` tags for React?_
BunGo completely removes the burden. It **auto-injects** the compiled JSX bundle, serialized handler JSON (`window.__BUNGO_DATA__`), and (in dev) the live-reload client by inserting a snippet **before the first `</head>`** in the rendered HTML; if there is no `</head>`, it falls back to **before `</body>`**, or appends to the document if neither tag exists.

Next: [React Integration](./react-integration.md).
