package domain

import (
	"bytes"
	"html/template"
)

var errorPageStyle = `*{margin:0;padding:0;box-sizing:border-box}` +
	`body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f172a;color:#e2e8f0;display:flex;align-items:center;justify-content:center;min-height:100vh}` +
	`.c{text-align:center;max-width:520px;padding:2rem}` +
	`.badge{display:inline-block;padding:.25rem .75rem;border-radius:9999px;font-size:.75rem;font-weight:600;letter-spacing:.05em;text-transform:uppercase;margin-bottom:1.5rem}` +
	`.err{background:#7f1d1d;color:#fca5a5}` +
	`.warn{background:#78350f;color:#fcd34d}` +
	`.info{background:#1e3a5f;color:#93c5fd}` +
	`h1{font-size:1.5rem;font-weight:700;margin-bottom:.5rem}` +
	`p{color:#94a3b8;margin-bottom:1.5rem;line-height:1.6}` +
	`code{background:#1e293b;padding:.25rem .5rem;border-radius:.375rem;font-size:.875rem;color:#c084fc;display:inline-block}` +
	`.hint{font-size:.875rem;color:#64748b;margin-top:1rem}`

var errorPageTmpl = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html>
<head>
  <title>{{.Title}}</title>
  <style>` + errorPageStyle + `</style>
</head>
<body>
  <div class="c">
    <span class="badge {{.BadgeClass}}">{{.Badge}}</span>
    <h1>{{.Heading}}</h1>
    <p>{{.Description}}</p>
    {{- range .Actions}}
    <p>{{.Label}}</p>
    <code>{{.Command}}</code>
    {{- end}}
    {{- if .Hint}}
    <p class="hint">{{.Hint}}</p>
    {{- end}}
  </div>
</body>
</html>`))

type errorPageData struct {
	Title       string
	Badge       string
	BadgeClass  string
	Heading     string
	Description string
	Actions     []errorPageAction
	Hint        string
}

type errorPageAction struct {
	Label   string
	Command string
}

func renderErrorPage(data errorPageData) []byte {
	var buf bytes.Buffer
	_ = errorPageTmpl.Execute(&buf, data)
	return buf.Bytes()
}

// GenerateErrorPages returns a map of filename → HTML content for nginx error pages.
func GenerateErrorPages(envName string) map[string][]byte {
	return map[string][]byte{
		"previewctl_502.html": renderErrorPage(errorPageData{
			Title:       "502 - Service Unavailable",
			Badge:       "502 Bad Gateway",
			BadgeClass:  "err",
			Heading:     "Service Unavailable",
			Description: "The service is started but not responding. It may still be booting, or it may have crashed.",
			Actions: []errorPageAction{
				{"Check the logs:", "previewctl -e " + envName + " env service logs"},
			},
			Hint: "If the service keeps crashing, try: previewctl -e " + envName + " env service restart",
		}),

		"previewctl_503.html": renderErrorPage(errorPageData{
			Title:       "503 - Service Not Started",
			Badge:       "503 Not Started",
			BadgeClass:  "warn",
			Heading:     "Service Not Started",
			Description: "This service exists in environment " + envName + " but has not been started yet.",
			Actions: []errorPageAction{
				{"Start it with:", "previewctl -e " + envName + " env service start <service>"},
			},
		}),

		"previewctl_404.html": renderErrorPage(errorPageData{
			Title:       "404 - Not Found",
			Badge:       "404 Not Found",
			BadgeClass:  "info",
			Heading:     "Unknown Service",
			Description: "No service matches this subdomain in environment " + envName + ".",
			Actions: []errorPageAction{
				{"List available services:", "previewctl -e " + envName + " env service list"},
			},
		}),
	}
}
