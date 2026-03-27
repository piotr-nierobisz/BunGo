package bungo

import (
	"bytes"
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

// buildScriptInjection builds template script injection markup and execution payload data.
// Inputs:
// - data: page data map that will be available to template execution and the browser.
// - inlineJS: compiled JavaScript bundle to inject inline as a module script.
// - moduleSrc: static module URL to inject instead of an inline bundle when provided.
// Outputs:
// - injectionStr: raw template snippet containing script tags for injection into HTML.
// - payload: template payload map including escaped script values and page data.
func buildScriptInjection(data map[string]any, inlineJS string, moduleSrc string) (injectionStr string, payload map[string]any) {
	var b strings.Builder
	if len(data) > 0 {
		b.WriteString("\n<script>window.__BUNGO_DATA__ = {{.BunGoInitialData}};</script>")
	}
	if inlineJS != "" {
		b.WriteString("\n<script type=\"module\">{{.BunGoInlineJS}}</script>\n")
	}
	if moduleSrc != "" {
		b.WriteString("\n<script type=\"module\" src=\"{{.BunGoModuleSrc}}\"></script>\n")
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
	if moduleSrc != "" {
		payload["BunGoModuleSrc"] = template.URL(moduleSrc)
	}
	if isDevModeEnabled() {
		payload["BunGoDevClientJS"] = template.JS(buildDevClientScript())
	}
	return b.String(), payload
}

// isDevModeEnabled reports whether BunGo dev mode is enabled for live-reload injection.
// Inputs:
// - none
// Outputs:
// - bool: true when BUNGO_DEV_ENABLED is set to "1"; otherwise false.
func isDevModeEnabled() bool {
	return os.Getenv("BUNGO_DEV_ENABLED") == "1"
}

// DevWebSocketPort is the fixed port the bungo CLI listens on for live reload; must match injected client.
const DevWebSocketPort = 35729

// buildDevClientScript returns the live-reload browser client script used in dev mode.
// Inputs:
// - none
// Outputs:
// - string: JavaScript source that reconnects to the BunGo dev websocket endpoint.
func buildDevClientScript() string {
	return `(function () {
  var hadDisconnect = false;
  var reconnectTimer = null;
  var ws = null;
  var appReadyPollActive = false;

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
    if (appReadyPollActive) return;
    appReadyPollActive = true;
    var delayMs = 200;
    var maxDelayMs = 1500;
    function tick() {
      fetch(window.location.href, { method: "GET", cache: "no-store", credentials: "same-origin" })
        .then(function () {
          window.location.reload();
        })
        .catch(function () {
          delayMs = Math.min(Math.floor(delayMs * 1.4), maxDelayMs);
          window.setTimeout(tick, delayMs);
        });
    }
    tick();
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

// injectScripts injects script markup into HTML before head/body closing tags when available.
// Inputs:
// - html: source HTML document string to modify.
// - injection: script markup snippet produced by buildScriptInjection.
// Outputs:
// - string: HTML with injected scripts, or the original HTML when injection is empty.
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

// RenderTemplate renders a page template, optionally composed inside a layout template.
// Inputs:
// - templatePath: required path to the page-specific .gohtml template file.
// - layoutPath: optional wrapper .gohtml path that defines {{block "content" .}}.
// - inlineJS: compiled JavaScript source to inject inline as a module script.
// - moduleSrc: optional static module URL injected when asset optimization is enabled.
// - data: template data passed to Execute after BunGo script fields are merged in.
// Outputs:
// - string: fully rendered HTML with BunGo data/scripts injected into template output.
// - error: non-nil when template files cannot be read, parsed, or executed.
func RenderTemplate(
	templatePath string,
	layoutPath string,
	inlineJS string,
	moduleSrc string,
	data map[string]any,
) (string, error) {
	injection, payload := buildScriptInjection(data, inlineJS, moduleSrc)

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
