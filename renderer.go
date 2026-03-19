package bungo

import (
	"bytes"
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

// buildScriptInjection returns the raw template snippet for __BUNGO_DATA__ and optional
// inline module script, and the payload map to pass to Execute. Callers must inject
// the snippet into the HTML and pass payload to the template.
func buildScriptInjection(data map[string]any, inlineJS string) (injectionStr string, payload map[string]any) {
	var b strings.Builder
	if len(data) > 0 {
		b.WriteString("\n<script>window.__BUNGO_DATA__ = {{.BunGoInitialData}};</script>")
	}
	if inlineJS != "" {
		b.WriteString("\n<script type=\"module\">{{.BunGoInlineJS}}</script>\n")
	}
	if isDevModeEnabled() {
		b.WriteString("\n<script>{{.BunGoDevClientJS}}</script>\n")
	}
	payload = make(map[string]any)
	for k, v := range data {
		payload[k] = v
	}
	if len(data) > 0 {
		dataJSON, _ := json.Marshal(data)
		payload["BunGoInitialData"] = template.JS(dataJSON)
	}
	if inlineJS != "" {
		payload["BunGoInlineJS"] = template.JS(inlineJS)
	}
	if isDevModeEnabled() {
		payload["BunGoDevClientJS"] = template.JS(buildDevClientScript())
	}
	return b.String(), payload
}

func isDevModeEnabled() bool {
	return os.Getenv("BUNGO_DEV_ENABLED") == "1"
}

// DevWebSocketPort is the fixed port the bungo CLI listens on for live reload; must match injected client.
const DevWebSocketPort = 35729

func buildDevClientScript() string {
	return `(function () {
  var hadDisconnect = false;
  var reconnectTimer = null;
  var ws = null;

  function endpoint() {
    var protocol = window.location.protocol === "https:" ? "wss" : "ws";
    return protocol + "://" + window.location.hostname + ":35729/__bungo";
  }

  function scheduleReconnect() {
    if (reconnectTimer !== null) return;
    reconnectTimer = window.setTimeout(function () {
      reconnectTimer = null;
      connect();
    }, 300);
  }

  function waitForAppReady() {
    fetch(window.location.href, { method: "HEAD", cache: "no-store" })
      .then(function (res) {
        if (res.ok) {
          window.location.reload();
        } else {
          setTimeout(waitForAppReady, 200);
        }
      })
      .catch(function () {
        setTimeout(waitForAppReady, 200);
      });
  }

  function connect() {
    ws = new WebSocket(endpoint());
    ws.onopen = function () {
      if (hadDisconnect) {
        waitForAppReady();
      }
    };
    ws.onclose = function () {
      hadDisconnect = true;
      scheduleReconnect();
    };
    ws.onerror = function () {
      if (ws) ws.close();
    };
  }

  connect();
})();`
}

// injectScripts inserts the injection string before the first </head> or </body>, or appends if neither is found.
func injectScripts(html string, injection string) string {
	if injection == "" {
		return html
	}
	if strings.Contains(html, "</head>") {
		return strings.Replace(html, "</head>", injection+"\n</head>", 1)
	}
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", injection+"\n</body>", 1)
	}
	return html + injection
}

// RenderTemplate renders the page using optional layout composition.
//
// templatePath is required: the page-specific .gohtml file. When layoutPath is empty,
// it is treated as a standalone page: scripts are injected and this template is executed.
//
// layoutPath is optional: when set, it is the wrapper .gohtml that defines {{block "content" .}}.
// The template file is expected to define {{define "content"}}...{{end}}. Scripts are
// injected into the layout (e.g. before </head> or </body>), and the layout is executed
// so that the block is filled by the template's "content" definition.
func RenderTemplate(
	templatePath string,
	layoutPath string,
	inlineJS string,
	data map[string]any,
) (string, error) {
	injection, payload := buildScriptInjection(data, inlineJS)

	if layoutPath == "" {
		// Standalone page: single template, inject scripts and execute.
		templateBytes, err := os.ReadFile(templatePath)
		if err != nil {
			return "", err
		}
		html := injectScripts(string(templateBytes), injection)
		tmpl, err := template.New(filepath.Base(templatePath)).Parse(html)
		if err != nil {
			return "", err
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, payload); err != nil {
			return "", err
		}
		return buf.String(), nil
	}

	// Layout composition: parse layout (with scripts injected) and template, then execute layout.
	layoutBytes, err := os.ReadFile(layoutPath)
	if err != nil {
		return "", err
	}
	layoutStr := injectScripts(string(layoutBytes), injection)

	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return "", err
	}
	templateStr := string(templateBytes)

	layoutName := filepath.Base(layoutPath)
	tmpl, err := template.New(layoutName).Parse(layoutStr)
	if err != nil {
		return "", err
	}
	tmpl, err = tmpl.Parse(templateStr)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, layoutName, payload); err != nil {
		return "", err
	}
	return buf.String(), nil
}
